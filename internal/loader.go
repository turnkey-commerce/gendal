package internal

import (
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"github.com/xo/dburl"

	"github.com/gedex/inflector"
	"github.com/knq/snaker"

	"github.com/turnkey-commerce/gendal/models"
)

// Loader is the common interface for database drivers that can generate code
// from a database schema.
type Loader interface {
	// NthParam returns the 0-based Nth param for the Loader.
	NthParam(i int) string

	// Mask returns the mask.
	Mask() string

	// Escape escapes the passed identifier based on its EscType.
	Escape(EscType, string) string

	// Relkind returns the schema's relkind identifier (ie, TABLE, VIEW, BASE TABLE, etc).
	Relkind(RelType) string

	// SchemaName loads the active schema name from the database if not provided on the cli.
	SchemaName(*ArgType) (string, error)

	// ParseQuery parses the ArgType.Query and writes any necessary type(s) to
	// the ArgType from the opened database handle.
	ParseQuery(*ArgType) error

	// LoadSchema loads the ArgType.Schema from the opened database handle,
	// writing any generated types to ArgType.
	LoadSchema(*ArgType) error
}

// SchemaLoaders are the available schema loaders.
var SchemaLoaders = map[string]Loader{}

// TypeLoader provides a common Loader implementation used by the built in
// schema/query loaders.
type TypeLoader struct {
	ParamN          func(int) string
	MaskFunc        func() string
	Esc             map[EscType]func(string) string
	ProcessRelkind  func(RelType) string
	Schema          func(*ArgType) (string, error)
	ParseType       func(*ArgType, string, bool) (int, string, string)
	EnumList        func(models.XODB, string) ([]*models.Enum, error)
	EnumValueList   func(models.XODB, string, string) ([]*models.EnumValue, error)
	ProcList        func(models.XODB, string) ([]*models.Proc, error)
	ProcParamList   func(models.XODB, string, string) ([]*models.ProcParam, error)
	TableList       func(models.XODB, string, string) ([]*models.Table, error)
	ColumnList      func(models.XODB, string, string) ([]*models.Column, error)
	ForeignKeyList  func(models.XODB, string, string) ([]*models.ForeignKey, error)
	IndexList       func(models.XODB, string, string) ([]*models.Index, error)
	IndexColumnList func(models.XODB, string, string, string) ([]*models.IndexColumn, error)
	QueryStrip      func([]string, []string)
	QueryColumnList func(*ArgType, []string) ([]*models.Column, error)
}

// NthParam satisifies Loader's NthParam.
func (tl TypeLoader) NthParam(i int) string {
	if tl.ParamN != nil {
		return tl.ParamN(i)
	}

	return fmt.Sprintf("$%d", i+1)
}

// Mask returns the parameter mask.
func (tl TypeLoader) Mask() string {
	if tl.MaskFunc != nil {
		return tl.MaskFunc()
	}

	return "$%d"
}

// Escape escapes the provided identifier based on the EscType.
func (tl TypeLoader) Escape(typ EscType, s string) string {
	if e, ok := tl.Esc[typ]; ok && e != nil {
		return e(s)
	}

	return `"` + s + `"`
}

// Relkind satisfies Loader's Relkind.
func (tl TypeLoader) Relkind(rt RelType) string {
	if tl.ProcessRelkind != nil {
		return tl.ProcessRelkind(rt)
	}

	return rt.String()
}

// SchemaName returns the active schema name.
func (tl TypeLoader) SchemaName(args *ArgType) (string, error) {
	if tl.Schema != nil {
		return tl.Schema(args)
	}

	return "", nil
}

// ParseQuery satisfies Loader's ParseQuery.
func (tl TypeLoader) ParseQuery(args *ArgType) error {
	var err error

	// parse supplied query
	queryStr, params := args.ParseQuery(tl.Mask(), true)
	inspectStr, _ := args.ParseQuery("NULL", false)

	// split up query and inspect based on lines
	query := strings.Split(queryStr, "\n")
	inspect := strings.Split(inspectStr, "\n")

	// query comment placeholder
	queryComments := make([]string, len(query)+1)

	// trim whitespace if applicable
	if args.QueryTrim {
		for n, l := range query {
			query[n] = strings.TrimSpace(l)
			if n < len(query)-1 {
				query[n] = query[n] + " "
			}
		}

		for n, l := range inspect {
			inspect[n] = strings.TrimSpace(l)
			if n < len(inspect)-1 {
				inspect[n] = inspect[n] + " "
			}
		}
	}

	// query strip
	if args.QueryStrip && tl.QueryStrip != nil {
		tl.QueryStrip(query, queryComments)
	}

	// create template for query type
	typeTpl := &Type{
		Name:    args.QueryType,
		RelType: Table,
		Fields:  []*Field{},
		Table: &models.Table{
			TableName: "[custom " + strings.ToLower(snaker.CamelToSnake(args.QueryType)) + "]",
		},
		Comment: args.QueryTypeComment,
	}

	if args.QueryFields == "" {
		// if no query fields specified, then pass to inspector
		colList, err := tl.QueryColumnList(args, inspect)
		if err != nil {
			return err
		}

		// process columns
		for _, c := range colList {
			f := &Field{
				Name: snaker.SnakeToCamelIdentifier(c.ColumnName),
				Col:  c,
			}
			f.Len, f.NilType, f.Type = tl.ParseType(args, c.DataType, args.QueryAllowNulls && !c.NotNull)
			typeTpl.Fields = append(typeTpl.Fields, f)
		}
	} else {
		// extract fields from query fields
		for _, qf := range strings.Split(args.QueryFields, ",") {
			qf = strings.TrimSpace(qf)
			colName := qf
			colType := "string"

			i := strings.Index(qf, " ")
			if i != -1 {
				colName = qf[:i]
				colType = qf[i+1:]
			}

			typeTpl.Fields = append(typeTpl.Fields, &Field{
				Name: colName,
				Type: colType,
				Col: &models.Column{
					ColumnName: snaker.CamelToSnake(colName),
				},
			})
		}
	}

	// generate query type template
	err = args.ExecuteTemplate(QueryTypeTemplate, args.QueryType, "", typeTpl)
	if err != nil {
		return err
	}

	// build func name
	funcName := args.QueryFunc
	if funcName == "" {
		// no func name specified, so generate based on type
		if args.QueryOnlyOne {
			funcName = args.QueryType
		} else {
			funcName = inflector.Pluralize(args.QueryType)
		}

		// affix any params
		if len(params) == 0 {
			funcName = "Get" + funcName
		} else {
			funcName = funcName + "By"
			for _, p := range params {
				funcName = funcName + strings.ToUpper(p.Name[:1]) + p.Name[1:]
			}
		}
	}

	// create func template
	queryTpl := &Query{
		Name:          funcName,
		Query:         query,
		QueryComments: queryComments,
		QueryParams:   params,
		OnlyOne:       args.QueryOnlyOne,
		Interpolate:   args.QueryInterpolate,
		Type:          typeTpl,
		Comment:       args.QueryFuncComment,
	}

	// generate template
	err = args.ExecuteTemplate(QueryTemplate, args.QueryType, "", queryTpl)
	if err != nil {
		return err
	}

	return nil
}

// LoadSchema loads schema definitions.
func (tl TypeLoader) LoadSchema(args *ArgType) error {
	var err error

	// load enums
	_, err = tl.LoadEnums(args)
	if err != nil {
		return err
	}

	// load procs
	_, err = tl.LoadProcs(args)
	if err != nil {
		return err
	}

	// load tables
	tableMap, err := tl.LoadRelkind(args, Table)
	if err != nil {
		return err
	}

	// load views
	viewMap, err := tl.LoadRelkind(args, View)
	if err != nil {
		return err
	}

	// merge views with the tableMap
	for k, v := range viewMap {
		tableMap[k] = v
	}

	// load foreign keys
	_, err = tl.LoadForeignKeys(args, tableMap)
	if err != nil {
		return err
	}

	// load indexes
	_, err = tl.LoadIndexes(args, tableMap)
	if err != nil {
		return err
	}

	return nil
}

// LoadEnums loads schema enums.
func (tl TypeLoader) LoadEnums(args *ArgType) (map[string]*Enum, error) {
	var err error

	// not supplied, so bail
	if tl.EnumList == nil {
		return nil, nil
	}

	// load enums
	enumList, err := tl.EnumList(args.DB, args.Schema)
	if err != nil {
		return nil, err
	}

	// process enums
	enumMap := map[string]*Enum{}
	for _, e := range enumList {
		enumTpl := &Enum{
			Name:              SingularizeIdentifier(e.EnumName),
			Schema:            args.Schema,
			Values:            []*EnumValue{},
			Enum:              e,
			ReverseConstNames: args.UseReversedEnumConstNames,
		}

		err = tl.LoadEnumValues(args, enumTpl)
		if err != nil {
			return nil, err
		}

		enumMap[enumTpl.Name] = enumTpl
		args.KnownTypeMap[enumTpl.Name] = true
	}

	// generate enum templates
	for _, e := range enumMap {
		err = args.ExecuteTemplate(EnumTemplate, e.Name, "", e)
		if err != nil {
			return nil, err
		}
	}

	return enumMap, nil
}

// LoadEnumValues loads schema enum values.
func (tl TypeLoader) LoadEnumValues(args *ArgType, enumTpl *Enum) error {
	var err error

	// load enum values
	enumValues, err := tl.EnumValueList(args.DB, args.Schema, enumTpl.Enum.EnumName)
	if err != nil {
		return err
	}

	// process enum values
	for _, ev := range enumValues {
		// chop off redundant enum name if applicable
		name := snaker.SnakeToCamelIdentifier(ev.EnumValue)
		if strings.HasSuffix(strings.ToLower(name), strings.ToLower(enumTpl.Name)) {
			n := name[:len(name)-len(enumTpl.Name)]
			if len(n) > 0 {
				name = n
			}
		}

		enumTpl.Values = append(enumTpl.Values, &EnumValue{
			Name: name,
			Val:  ev,
		})
	}

	return nil
}

// LoadProcs loads schema stored procedures definitions.
func (tl TypeLoader) LoadProcs(args *ArgType) (map[string]*Proc, error) {
	var err error

	// not supplied, so bail
	if tl.ProcList == nil {
		return nil, nil
	}

	// load procs
	procList, err := tl.ProcList(args.DB, args.Schema)
	if err != nil {
		return nil, err
	}

	// process procs
	procMap := map[string]*Proc{}
	for _, p := range procList {
		// fix the name if it starts with one or more underscores
		name := p.ProcName
		for strings.HasPrefix(name, "_") {
			name = name[1:]
		}

		// create template
		procTpl := &Proc{
			Name:   snaker.SnakeToCamelIdentifier(name),
			Schema: args.Schema,
			Params: []*Field{},
			Return: &Field{},
			Proc:   p,
		}

		// parse return type into template
		// TODO: fix this so that nullable types can be returned
		_, procTpl.Return.NilType, procTpl.Return.Type = tl.ParseType(args, p.ReturnType, false)

		// load proc parameters
		err = tl.LoadProcParams(args, procTpl)
		if err != nil {
			return nil, err
		}

		procMap[p.ProcName] = procTpl
	}

	// generate proc templates
	for _, p := range procMap {
		err = args.ExecuteTemplate(ProcTemplate, "sp_"+p.Name, "", p)
		if err != nil {
			return nil, err
		}
	}

	return procMap, nil
}

// LoadProcParams loads schema stored procedure parameters.
func (tl TypeLoader) LoadProcParams(args *ArgType, procTpl *Proc) error {
	var err error

	// load proc params
	paramList, err := tl.ProcParamList(args.DB, args.Schema, procTpl.Proc.ProcName)
	if err != nil {
		return err
	}

	// process params
	for i, p := range paramList {
		// TODO: some databases support named parameters in procs (MySQL)
		paramTpl := &Field{
			Name: fmt.Sprintf("v%d", i),
		}

		// TODO: fix this so that nullable types can be used as parameters
		_, _, paramTpl.Type = tl.ParseType(args, strings.TrimSpace(p.ParamType), false)

		// add to proc params
		if procTpl.ProcParams != "" {
			procTpl.ProcParams = procTpl.ProcParams + ", "
		}
		procTpl.ProcParams = procTpl.ProcParams + p.ParamType

		procTpl.Params = append(procTpl.Params, paramTpl)
	}

	return nil
}

// LoadRelkind loads a schema table/view definition.
func (tl TypeLoader) LoadRelkind(args *ArgType, relType RelType) (map[string]*Type, error) {
	var err error

	// load tables
	tableList, err := tl.TableList(args.DB, args.Schema, tl.Relkind(relType))
	if err != nil {
		return nil, err
	}

	// tables
	tableMap := make(map[string]*Type)
	for _, ti := range tableList {
		ignore := false
		for _, ignoreTable := range args.IgnoreTables {
			if strings.ToLower(ignoreTable) == strings.ToLower(ti.TableName) {
				// Skip adding this table if users has specified ignore
				ignore = true
				break
			}
		}

		if ignore {
			continue
		}

		// create template
		typeTpl := &Type{
			Name:    SingularizeIdentifier(ti.TableName),
			Schema:  args.Schema,
			RelType: relType,
			Fields:  []*Field{},
			Table:   ti,
		}

		// process columns
		err = tl.LoadColumns(args, typeTpl)
		if err != nil {
			return nil, err
		}

		tableMap[ti.TableName] = typeTpl
	}

	// generate table templates
	for _, t := range tableMap {
		t.Sqlx = args.Sqlx
		// If args.Sqlx is true, get foreign keys for current table and add to our type
		if args.Sqlx {
			foreignKeyList, err := tl.ForeignKeyList(args.DB, args.Schema, t.Table.TableName)

			if err != nil {
				return nil, err
			}

			// Hack function used to get proper foreign keys from cockroachdb
			foreignKeyList, err = tl.extractProperForeignKeys(foreignKeyList, args.DSN)

			// Using GetTableForeignKeys to retrieve slice of internal#ForeignKey on per table basis
			t.ForeignKeys, err = tl.GetTableForeignKeys(args, tableMap, t, nil, foreignKeyList)

			if err != nil {
				return nil, err
			}
		}

		err = args.ExecuteTemplate(TypeTemplate, t.Name, "", t)
		if err != nil {
			return nil, err
		}
	}

	return tableMap, nil
}

// LoadColumns loads schema table/view columns.
func (tl TypeLoader) LoadColumns(args *ArgType, typeTpl *Type) error {
	var err error

	// load columns
	columnList, err := tl.ColumnList(args.DB, args.Schema, typeTpl.Table.TableName)
	if err != nil {
		return err
	}

	// process columns
	for _, c := range columnList {
		ignore := false

		for _, ignoreField := range args.IgnoreFields {
			if strings.ToLower(ignoreField) == strings.ToLower(c.ColumnName) {
				// Skip adding this field if user has specified they are not
				// interested.
				//
				// This could be useful for fields which are managed by the
				// database (e.g. automatically updated timestamps) instead of
				// via Go code.
				ignore = true
			}
		}

		if ignore {
			continue
		}

		// set col info
		f := &Field{
			Name: snaker.SnakeToCamelIdentifier(c.ColumnName),
			Col:  c,
		}
		f.Len, f.NilType, f.Type = tl.ParseType(args, c.DataType, !c.NotNull)

		// set primary key
		if c.IsPrimaryKey {
			typeTpl.PrimaryKeyFields = append(typeTpl.PrimaryKeyFields, f)
			// This is retained for backward compatibility in the templates.
			typeTpl.PrimaryKey = f
		}

		// append col to template fields
		typeTpl.Fields = append(typeTpl.Fields, f)
	}

	return nil
}

// LoadForeignKeys loads foreign keys.
func (tl TypeLoader) LoadForeignKeys(args *ArgType, tableMap map[string]*Type) (map[string]*ForeignKey, error) {
	var err error

	fkMap := map[string]*ForeignKey{}
	for _, t := range tableMap {
		// load keys per table
		err = tl.LoadTableForeignKeys(args, tableMap, t, fkMap)
		if err != nil {
			return nil, err
		}
	}

	// determine foreign key names
	for _, fk := range fkMap {
		fk.Name = args.ForeignKeyName(fkMap, fk)
	}

	// generate templates
	for _, fk := range fkMap {
		err = args.ExecuteTemplate(ForeignKeyTemplate, fk.Type.Name, fk.ForeignKey.ForeignKeyName, fk)
		if err != nil {
			return nil, err
		}
	}

	return fkMap, nil
}

// LoadTableForeignKeys loads schema foreign key definitions per table.
func (tl TypeLoader) LoadTableForeignKeys(args *ArgType, tableMap map[string]*Type, typeTpl *Type, fkMap map[string]*ForeignKey) error {
	var err error

	// load foreign keys
	foreignKeyList, err := tl.ForeignKeyList(args.DB, args.Schema, typeTpl.Table.TableName)
	if err != nil {
		return err
	}

	// Hack function used to get proper foreign keys from cockroachdb
	foreignKeyList, err = tl.extractProperForeignKeys(foreignKeyList, args.DSN)

	if err != nil {
		return err
	}

	// Using GetTableForeignKeys here to modify fkmap to get all foreignkeys from database
	_, err = tl.GetTableForeignKeys(args, tableMap, typeTpl, fkMap, foreignKeyList)

	if err != nil {
		return err
	}

	return nil
}

// LoadIndexes loads schema index definitions.
func (tl TypeLoader) LoadIndexes(args *ArgType, tableMap map[string]*Type) (map[string]*Index, error) {
	var err error

	ixMap := map[string]*Index{}
	for _, t := range tableMap {
		// load table indexes
		err = tl.LoadTableIndexes(args, t, ixMap)
		if err != nil {
			return nil, err
		}
	}

	// generate templates
	for _, ix := range ixMap {
		err = args.ExecuteTemplate(IndexTemplate, ix.Type.Name, ix.Index.IndexName, ix)
		if err != nil {
			return nil, err
		}
	}

	return ixMap, nil
}

// LoadTableIndexes loads schema index definitions per table.
func (tl TypeLoader) LoadTableIndexes(args *ArgType, typeTpl *Type, ixMap map[string]*Index) error {
	var err error
	var priIxLoaded bool

	// load indexes
	indexList, err := tl.IndexList(args.DB, args.Schema, typeTpl.Table.TableName)
	if err != nil {
		return err
	}

	// process indexes
	for _, ix := range indexList {
		// save whether or not the primary key index was processed
		priIxLoaded = priIxLoaded || ix.IsPrimary || (ix.Origin == "pk")

		// create index template
		ixTpl := &Index{
			Schema: args.Schema,
			Type:   typeTpl,
			Fields: []*Field{},
			Index:  ix,
		}

		// load index columns
		err = tl.LoadIndexColumns(args, ixTpl)
		if err != nil {
			return err
		}

		// build func name
		args.BuildIndexFuncName(ixTpl)

		ixMap[typeTpl.Table.TableName+"_"+ix.IndexName] = ixTpl
	}

	// search for primary key if it was skipped being set in the type
	pk := typeTpl.PrimaryKey
	if pk == nil {
		for _, f := range typeTpl.Fields {
			if f.Col.IsPrimaryKey {
				pk = f
				break
			}
		}
	}

	// if no primary key index loaded, but a primary key column was defined in
	// the type, then create the definition here. this is needed for sqlite, as
	// sqlite doesn't define primary keys in its index list
	if args.LoaderType != "ora" && !priIxLoaded && pk != nil {
		ixName := typeTpl.Table.TableName + "_" + pk.Col.ColumnName + "_pkey"
		ixMap[ixName] = &Index{
			FuncName: typeTpl.Name + "By" + pk.Name,
			Schema:   args.Schema,
			Type:     typeTpl,
			Fields:   []*Field{pk},
			Index: &models.Index{
				IndexName: ixName,
				IsUnique:  true,
				IsPrimary: true,
			},
		}
	}

	return nil
}

// LoadIndexColumns loads the index column information.
func (tl TypeLoader) LoadIndexColumns(args *ArgType, ixTpl *Index) error {
	var err error

	// load index columns
	indexCols, err := tl.IndexColumnList(args.DB, args.Schema, ixTpl.Type.Table.TableName, ixTpl.Index.IndexName)
	if err != nil {
		return err
	}

	// process index columns
	for _, ic := range indexCols {
		var field *Field

	fieldLoop:
		// find field
		for _, f := range ixTpl.Type.Fields {
			if f.Col.ColumnName == ic.ColumnName {
				field = f
				break fieldLoop
			}
		}

		if field == nil {
			continue
		}

		ixTpl.Fields = append(ixTpl.Fields, field)
	}

	return nil
}

// GetTableForeignKeys returns a slice of internal#ForeignKey based on the parameters passed
// GetTableForeignKeys also modifies fkMap to add internal#ForeignKey to map
// fkMap can be nil
func (tl TypeLoader) GetTableForeignKeys(
	args *ArgType,
	tableMap map[string]*Type,
	typeTpl *Type,
	fkMap map[string]*ForeignKey,
	foreignKeyList []*models.ForeignKey,
) ([]*ForeignKey, error) {
	foreignKeys := make([]*ForeignKey, 0)

	// loop over foreign keys for table
	for _, fk := range foreignKeyList {
		var refTpl *Type
		var col, refCol *Field

	colLoop:
		// find column
		for _, f := range typeTpl.Fields {
			if f.Col.ColumnName == fk.ColumnName {
				col = f
				break colLoop
			}
		}

	refTplLoop:
		// find ref table
		for _, t := range tableMap {
			if t.Table.TableName == fk.RefTableName {
				refTpl = t
				break refTplLoop
			}
		}

	refColLoop:
		// find ref column
		for _, f := range refTpl.Fields {
			if f.Col.ColumnName == fk.RefColumnName {
				refCol = f
				break refColLoop
			}
		}

		// no ref col, but have ref tpl, so use primary key
		if refTpl != nil && refCol == nil {
			refCol = refTpl.PrimaryKey
		}

		// check everything was found
		if col == nil || refTpl == nil || refCol == nil {
			return nil, errors.New("could not find col, refTpl, or refCol")
		}

		// foreign key name
		if fk.ForeignKeyName == "" {
			fk.ForeignKeyName = typeTpl.Table.TableName + "_" + col.Col.ColumnName + "_fkey"
		}

		foreignKey := &ForeignKey{
			Schema:     args.Schema,
			Type:       typeTpl,
			Field:      col,
			RefType:    refTpl,
			RefField:   refCol,
			ForeignKey: fk,
		}

		if fkMap != nil {
			// This random generated number is included in fkmap's key name as
			// cockroachdb does not have database wide unique foreign key names
			// so if two tables have same foreign key column name and reference
			// same table the names are the same which get overridden in our
			// fkmap variable
			randNum := "_" + strconv.Itoa(rand.Int())

			// fkMap is used for getting all foreign keys in database to be used for
			// foreign key functions in *.foreignkey.go.tpl
			fkMap[fk.ForeignKeyName+randNum] = foreignKey
		}

		// foreignKeys is used if args.Sqlx is set to true and returns foreign keys
		// for one table at a time
		foreignKeys = append(foreignKeys, foreignKey)
	}

	return foreignKeys, nil
}

// extractProperForeignKeys takes given models#ForeignKey slice and takes out foreign keys
// that should not be in list
// This is hack function for cockroachdb as cockroachdb adds foreignkeys that don't exist
// when using the unique() function at database level
func (tl TypeLoader) extractProperForeignKeys(fkList []*models.ForeignKey, dsn string) ([]*models.ForeignKey, error) {
	url, err := dburl.Parse(dsn)

	if err != nil {
		return nil, err
	}

	isCockroachDB := false

	if url.Unaliased == "cockroachdb" {
		isCockroachDB = true
	}

	if isCockroachDB {
		newForeignKeyList := make([]*models.ForeignKey, 0)

		// This for loop is a "hack" to create new list of appropriate foreign keys references based on list passed
		// If you use the unique() function in your database schema for two or more foreign key ids
		// (most commonly used in a join table), the query used in the TypeLoader#ForeignKeyList function
		// adds the unique constraint as a foreign key which causes duplicate variable names
		// This only happens in cockroachdb which makes me believe that this might some type of
		// bug with cockroachdb
		// If a future commit fixes the query to get same result, we could get rid of this function
		for i := range fkList {
			if strings.Contains(fkList[i].ForeignKeyName, fkList[i].ColumnName) {
				newForeignKeyList = append(newForeignKeyList, fkList[i])
			}
		}

		return newForeignKeyList, nil
	}

	return fkList, nil
}
