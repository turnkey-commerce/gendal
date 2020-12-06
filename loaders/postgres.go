package loaders

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	_ "github.com/lib/pq"

	"github.com/knq/snaker"

	"github.com/turnkey-commerce/gendal/internal"
	"github.com/turnkey-commerce/gendal/models"
)

func init() {
	internal.SchemaLoaders["postgres"] = internal.TypeLoader{
		ProcessRelkind:  PgRelkind,
		Schema:          func(*internal.ArgType) (string, error) { return "public", nil },
		ParseTypeFunc:   PgParseType,
		EnumList:        models.PgEnums,
		EnumValueList:   models.PgEnumValues,
		ProcList:        PgProcs,
		ProcParamList:   models.PgProcParams,
		TableList:       PgTables,
		ColumnList:      PgTableColumns,
		ForeignKeyList:  models.PgTableForeignKeys,
		IndexList:       models.PgTableIndexes,
		IndexColumnList: PgIndexColumns,
		QueryStrip:      PgQueryStrip,
		QueryColumnList: PgQueryColumns,
	}
}

// PgRelkind returns the postgres string representation for RelType.
func PgRelkind(relType internal.RelType) string {
	var s string
	switch relType {
	case internal.Table:
		s = "r"
	case internal.View:
		s = "v"
	default:
		panic("unsupported RelType")
	}
	return s
}

// PgParseType parse a postgres type into a Go type based on the column
// definition.
func PgParseType(args *internal.ArgType, dt string, nullable bool) (int, string, string) {
	precision := 0
	asSlice := false

	// handle SETOF
	if strings.HasPrefix(dt, "SETOF ") {
		_, _, t := PgParseType(args, dt[len("SETOF "):], false)
		return 0, "nil", "[]" + t
	}

	// determine if it's a slice
	if strings.HasSuffix(dt, "[]") {
		dt = dt[:len(dt)-2]
		asSlice = true
	}

	// extract precision
	dt, precision, _ = args.ParsePrecision(dt)

	typ, nilVal, ok := args.PgtypeMode.PostgresNameToPgtypeName(
		dt,
		nullable,
		&asSlice,
		args.Int32Type,
		args.Uint32Type,
	)

	if !ok {
		if strings.HasPrefix(dt, args.Schema+".") {
			// in the same schema, so chop off
			typ = snaker.SnakeToCamelIdentifier(dt[len(args.Schema)+1:])
			nilVal = typ + "(0)"
		} else {
			typ = snaker.SnakeToCamelIdentifier(dt)
			nilVal = typ + "{}"
		}
	}

	// special case for []slice
	if typ == "string" && asSlice {
		return precision, "StringSlice{}", "StringSlice"
	}

	// correct type if slice
	if asSlice {
		typ = "[]" + typ
		nilVal = "nil"
	}

	return precision, nilVal, typ
}

// pgQueryStripRE is the regexp to match the '::type AS name' portion in a query,
// which is a quirk/requirement of generating queries as is done in this
// package.
var pgQueryStripRE = regexp.MustCompile(`(?i)::[a-z][a-z0-9_\.]+\s+AS\s+[a-z][a-z0-9_\.]+`)

// PgQueryStrip strips stuff.
func PgQueryStrip(query []string, queryComments []string) {
	for i, l := range query {
		pos := pgQueryStripRE.FindStringIndex(l)
		if pos != nil {
			query[i] = l[:pos[0]] + l[pos[1]:]
			queryComments[i+1] = l[pos[0]:pos[1]]
		} else {
			queryComments[i+1] = ""
		}
	}
}

// PgProcs returns the Postgres procs after checking whether the
// pg_get_function_result function exits (it is missing on CockRoachDB)
func PgProcs(db models.XODB, schema string) ([]*models.Proc, error) {
	var err error
	res := []*models.Proc{}

	// Check if the get function result function is supported
	funcExists, err := models.PgGetFuncResExists(db)
	if err != nil {
		return nil, err
	}

	// Get the results from PgProcs only if the required function exists.
	if len(funcExists) == 1 && funcExists[0].Exists {
		res, err = models.PgProcs(db, schema)
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

// PgTables returns the Postgres tables with the manual PK information added.
// ManualPk is true when the table does not have a sequence defined.
func PgTables(db models.XODB, schema string, relkind string) ([]*models.Table, error) {
	var err error

	// get the tables
	rows, err := models.PgTables(db, schema, relkind)
	if err != nil {
		return nil, err
	}

	// Get the tables that have a sequence defined.
	sequences, err := models.PgSequences(db, schema)
	if err != nil {
		// Set it to an empty set on error.
		sequences = []*models.Sequence{}
	}

	// Get the tables that alternatively have a Cockroach autoincrement defined.
	crAutoIncrements, err := models.CrAutoIncrements(db, schema)
	if err != nil {
		// Set it to an empty set on error.
		crAutoIncrements = []*models.CrAutoincrement{}
	}

	// Add information about manual FK.
	var tables []*models.Table
	for _, row := range rows {
		manualPk := true
		// Look for a match in the table name where it contains the sequence
		// or a CockRoachDB autoincrement table.
		for _, sequence := range sequences {
			if sequence.TableName == row.TableName {
				manualPk = false
			}
		}
		for _, crAutoInc := range crAutoIncrements {
			if crAutoInc.TableName == row.TableName {
				manualPk = false
			}
		}
		tables = append(tables, &models.Table{
			TableName: row.TableName,
			Type:      row.Type,
			ManualPk:  manualPk,
		})
	}

	return tables, nil
}

// PgQueryColumns parses the query and generates a type for it.
func PgQueryColumns(args *internal.ArgType, inspect []string) ([]*models.Column, error) {
	var err error

	// create temporary view xoid
	// modified to create a view to be deleted as TEMPORARY isn't supported by CockroachDB
	xoid := "_xo_" + internal.GenRandomID()
	viewq := `CREATE VIEW ` + xoid + ` AS (` + strings.Join(inspect, "\n") + `)`
	models.XOLog(viewq)
	_, err = args.DB.Exec(viewq)
	if err != nil {
		return nil, err
	}

	// query to determine schema name where view was created
	var nspq = `SELECT n.nspname ` +
		`FROM pg_class c ` +
		`JOIN pg_namespace n ON n.oid = c.relnamespace ` +
		`WHERE c.relname = $1`

	// run query
	var schema string
	models.XOLog(nspq, xoid)
	err = args.DB.QueryRow(nspq, xoid).Scan(&schema)
	if err != nil {
		return nil, err
	}

	// load column information
	result, err := models.PgTableColumns(args.DB, schema, xoid, false)

	for _, col := range result {
		if col.DataType == "json" || col.DataType == "jsonb" {
			col.DataType = col.ColumnName
		}
	}

	// delete temporary view
	var dropq = `DROP VIEW ` + xoid
	models.XOLog(dropq)
	_, err = args.DB.Exec(dropq)
	if err != nil {
		return nil, err
	}
	return result, err
}

// PgIndexColumns returns the column list for an index.
func PgIndexColumns(db models.XODB, schema string, table string, index string) ([]*models.IndexColumn, error) {
	var err error

	// load columns
	cols, err := models.PgIndexColumns(db, schema, index)
	if err != nil {
		return nil, err
	}

	// load col order
	colOrd, err := models.PgGetColOrder(db, schema, index)
	if err != nil {
		return nil, err
	}

	// build schema name used in errors
	s := schema
	if s != "" {
		s = s + "."
	}

	// put cols in order using colOrder
	ret := []*models.IndexColumn{}
	for _, v := range strings.Split(colOrd.Ord, " ") {
		cid, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("could not convert %s%s index %s column %s to int", s, table, index, v)
		}

		// find column
		found := false
		var c *models.IndexColumn
		for _, ic := range cols {
			if cid == ic.Cid {
				found = true
				c = ic
				break
			}
		}

		// sanity check
		if !found {
			return nil, fmt.Errorf("could not find %s%s index %s column id %d", s, table, index, cid)
		}

		ret = append(ret, c)
	}

	return ret, nil
}

func PgTableColumns(db models.XODB, schema string, table string) ([]*models.Column, error) {
	cols, err := models.PgTableColumns(db, schema, table, internal.Args.EnablePostgresOIDs)
	if err != nil {
		return nil, err
	}

	if !internal.Args.EnablePostgresJson {
		return cols, nil
	}

	for _, col := range cols {
		if col.DataType == "json" || col.DataType == "jsonb" {
			col.DataType = col.ColumnName
		}
	}

	return cols, nil
}
