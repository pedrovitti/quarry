package exec

import (
	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/catalog"
	"github.com/pedrovitti/quarry/internal/storage"
	"github.com/pedrovitti/quarry/internal/types"
)

// alterState is the table as the actions of one ALTER TABLE see it: staged
// copies of the name, the columns and the rows, mutated action by action and
// written back only when every action has succeeded.
type alterState struct {
	name        string
	columns     []catalog.Column
	rows        []storage.Row
	rowsChanged bool
}

func (s *alterState) find(name string) (int, error) {
	for i, column := range s.columns {
		if column.Name == name {
			return i, nil
		}
	}
	return 0, errorf("table %s has no column %q", s.name, name)
}

func (s *alterState) has(name string) bool {
	for _, column := range s.columns {
		if column.Name == name {
			return true
		}
	}
	return false
}

func (s *alterState) binding() *Binding {
	columns := make([]ColumnInfo, len(s.columns))
	for i, column := range s.columns {
		columns[i] = ColumnInfo{Table: s.name, Name: column.Name, Kind: column.Kind()}
	}
	return NewBinding(columns)
}

func (e *Engine) runAlterTable(stmt *ast.AlterTable, params []types.Value) (*Result, error) {
	table, err := e.catalog.Get(stmt.Table)
	if err != nil {
		return nil, errorf("%s", err)
	}
	heap, err := e.store.Get(stmt.Table)
	if err != nil {
		return nil, errorf("%s", err)
	}

	state := &alterState{
		name:    stmt.Table,
		columns: table.Schema.Columns(),
		rows:    heap.Rows(),
	}
	for _, action := range stmt.Actions {
		if err := e.applyAlterAction(state, action, params); err != nil {
			return nil, err
		}
	}

	schema, err := catalog.NewSchema(state.columns)
	if err != nil {
		return nil, errorf("%s", err)
	}

	if state.name != stmt.Table {
		if err := e.catalog.Rename(stmt.Table, state.name); err != nil {
			return nil, errorf("%s", err)
		}
		if err := e.store.Rename(stmt.Table, state.name); err != nil {
			return nil, errorf("%s", err)
		}
	}
	table.Schema = schema
	if state.rowsChanged {
		heap.Truncate()
		for _, row := range state.rows {
			heap.Insert(row)
		}
	}
	return &Result{Tag: "ALTER TABLE"}, nil
}

func (e *Engine) applyAlterAction(state *alterState, action ast.AlterAction, params []types.Value) error {
	switch act := action.(type) {
	case *ast.RenameTable:
		if act.To == state.name || e.catalog.Has(act.To) {
			return errorf("table %q already exists", act.To)
		}
		state.name = act.To
		return nil

	case *ast.RenameColumn:
		i, err := state.find(act.From)
		if err != nil {
			return err
		}
		if state.has(act.To) {
			return errorf("column %q already exists", act.To)
		}
		state.columns[i].Name = act.To
		return nil

	case *ast.AddColumn:
		return e.addColumn(state, act, params)

	case *ast.DropColumn:
		i, err := state.find(act.Name)
		if err != nil {
			return err
		}
		if len(state.columns) == 1 {
			return errorf("cannot drop the only column of table %q", state.name)
		}
		state.columns = append(state.columns[:i], state.columns[i+1:]...)
		for j, row := range state.rows {
			state.rows[j] = append(row[:i], row[i+1:]...)
		}
		state.rowsChanged = true
		return nil

	case *ast.SetNotNull:
		i, err := state.find(act.Column)
		if err != nil {
			return err
		}
		if act.NotNull {
			for _, row := range state.rows {
				if row[i].IsNull() {
					return errorf("column %q must not be NULL", act.Column)
				}
			}
		}
		state.columns[i].NotNull = act.NotNull
		return nil

	case *ast.SetColumnType:
		return e.setColumnType(state, act, params)
	}
	return errorf("cannot execute this kind of action")
}

func (e *Engine) addColumn(state *alterState, act *ast.AddColumn, params []types.Value) error {
	if state.has(act.Def.Name) {
		return errorf("column %q already exists", act.Def.Name)
	}
	column := catalog.ColumnFromDef(act.Def)

	fill := types.Null()
	if act.Default != nil {
		compiled, err := Compile(act.Default, NewBinding(nil), params)
		if err != nil {
			return err
		}
		value, err := compiled.Eval(storage.Row{})
		if err != nil {
			return err
		}
		fill = value
	}

	if !fill.IsNull() {
		converted, err := types.Convert(fill, column.Kind())
		if err != nil {
			return errorf("column %q: %s", column.Name, err)
		}
		fill = converted
	} else if column.NotNull && len(state.rows) > 0 {
		return errorf("column %q must not be NULL", column.Name)
	}

	state.columns = append(state.columns, column)
	for j, row := range state.rows {
		state.rows[j] = append(row, fill)
	}
	if len(state.rows) > 0 {
		state.rowsChanged = true
	}
	return nil
}

func (e *Engine) setColumnType(state *alterState, act *ast.SetColumnType, params []types.Value) error {
	i, err := state.find(act.Column)
	if err != nil {
		return err
	}
	want := types.KindOf(act.Type)

	var compiled Expr
	if act.Using != nil {
		compiled, err = Compile(act.Using, state.binding(), params)
		if err != nil {
			return err
		}
	}

	for j, row := range state.rows {
		value := row[i]
		if compiled != nil {
			value, err = compiled.Eval(row)
			if err != nil {
				return err
			}
		}
		if value.IsNull() {
			if state.columns[i].NotNull {
				return errorf("column %q must not be NULL", act.Column)
			}
			state.rows[j][i] = value
			continue
		}
		converted, err := types.Convert(value, want)
		if err != nil {
			return errorf("column %q: %s", act.Column, err)
		}
		state.rows[j][i] = converted
	}

	state.columns[i].Type = act.Type
	if len(state.rows) > 0 {
		state.rowsChanged = true
	}
	return nil
}
