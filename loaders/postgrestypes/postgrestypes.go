package postgrestypes

import (
	"strings"
	"errors"
)

// PgtypeMode represents the way types should be generated for postgres.
type PgtypeMode int

const (
	// PgtypeModeStd is the default mode. It will generate code using the types
	// from the `sql/database` module.
	PgtypeModeStd PgtypeMode = iota

	// PgtypeModeFull will generate code using types from the `pgtype` module
	// for every fields and arguments. This mode lets `pgx` use `pgtype` to
	// its fullest but the models can look a bit less clean as each field is
	// a wrapped type.
	PgtypeModeFull

	// PgtypeModePointer will generate code using the types intornal to the
	// `pgtype` structures. For nullable fields and arguments, this type will
	// be a pointer to that type. If the internal type of the struct cannot be
	// used because the struct contains several fields, the equivalent from
	// `PgTypeModeStd` is used instead.
	PgtypeModePointer

	// Same as `PgtypeModePointer` except that it will use the `pgtype`
	// structures for nullable fields and arguments.
	PgtypeModePgtype
)

// UnmarshalText unmarshals PgtypeMode from text.
func (p *PgtypeMode) UnmarshalText(text []byte) error {
	switch strings.ToLower(string(text)) {
	case "std", "default":
		*p = PgtypeModeStd
	case "pgtype-full":
		*p = PgtypeModeFull
	case "pointer":
		*p = PgtypeModePointer
	case "pgtype":
		*p = PgtypeModePgtype
	default:
		return errors.New("invalid PgtypeMode")
	}

	return nil
}

// String satisfies the Stringer interface.
func (p PgtypeMode) String() string {
	switch p {
	case PgtypeModeStd:
		return "std"
	case PgtypeModeFull:
		return "pgtype-full"
	case PgtypeModePointer:
		return "pointer"
	case PgtypeModePgtype:
		return "pgtype"
	}

	return "unknown"
}

func (p PgtypeMode) PostgresNameToPgtypeName(dt string, nullable bool, asSlice *bool, int32Type string, uint32Type string) (typ string, nilVal string, ok bool) {
	switch p {
	case PgtypeModeStd:
		return postgresNameToGoName(dt, nullable, asSlice, int32Type, uint32Type)
	case PgtypeModeFull:
		return postgresNameToPgtypeName(dt)
	case PgtypeModePointer:
		typ, nilVal, ok = postgresNameToInternalPgtypeName(dt, asSlice, int32Type, uint32Type)
		if nullable {
			return "*" + typ, "nil", ok
		} else {
			return typ, nilVal, ok
		}
	case PgtypeModePgtype:
		if nullable {
			return postgresNameToPgtypeName(dt)
		} else {
			return postgresNameToInternalPgtypeName(dt, asSlice, int32Type, uint32Type)
		}
	default:
		panic("Unreachable default in PostgresNameToPgtypeName switch.")
	}
}

func postgresNameToInternalPgtypeName(dt string, asSlice *bool, int32Type string, uint32Type string) (typ string, nilVal string, ok bool) {
	switch dt {
	case "boolean":
		typ = "bool"
		nilVal = "false"
	case "inet":
		typ = "*net.IPNet"
		nilVal = "nil"
	case "character varying":
		typ = "string"
		nilVal = `""`
	case "money", "text", "character":
		typ = "string"
		nilVal = `""`
	case "smallint", "smallserial":
		typ = "int16"
		nilVal = "0"
	case "integer":
		typ = int32Type
		nilVal = "0"
	case "serial":
		typ = uint32Type
		nilVal = "0"
	case "bigint", "bigserial":
		typ = "int64"
		nilVal = "0"
	case "real":
		typ = "float32"
		nilVal = "0"
	case "double precision":
		typ = "float64"
		nilVal = "0"
	case "numeric":
		typ = "float64"
		nilVal = "0.0"
	case "bytea":
		typ = "[]byte"
		nilVal = "[]"
	case "date", "timestamp without time zone", "timestamp with time zone":
		typ = "time.Time"
		nilVal = "time.Time{}"
	case "time with time zone", "time without time zone":
		// pgtype uses the same type for both types as postgres' type
		// `time with time zone` does not actually store a time zone.
		// Source: https://github.com/jackc/pgtype/issues/14#issuecomment-559316485
		typ = "int64"
		nilVal = "0"
	case "interval":
		typ = "*time.Duration"
		nilVal = "nil"
	case `"char"`:
		typ = "int8"
		nilVal = "0"
	case "bit":
		typ = "uint8"
		nilVal = `uint8(0)`
	case "bit varying", `"any"`:
		typ = "byte"
		*asSlice = true
		nilVal = "nil"
	case "hstore":
		typ = "hstore.Hstore"
		nilVal = "nil"
	case "uuid":
		typ = "[16]byte"
		nilVal = "uuid.New()"
	default:
		return "", "", false
	}
	return typ, nilVal, true
}

func postgresNameToPgtypeName(dt string) (typ string, nilVal string, ok bool) {
	switch dt {
	case "boolean":
		typ = "pgtype.Bool"
	case "inet":
		typ = "pgtype.Inet"
	case "character varying":
		typ = "pgtype.Varchar"
	case "money", "text", "character":
		typ = "pgtype.Text"
	case "smallint", "smallserial":
		typ = "pgtype.Int2"
	case "integer", "serial":
		typ = "pgtype.Int4"
	case "bigint", "bigserial":
		typ = "pgtype.Int8"
	case "real":
		typ = "pgtype.Float4"
	case "double precision":
		typ = "pgtype.Float8"
	case "numeric":
		typ = "pgtype.Numeric"
	case "bytea":
		typ = "pgtype.Bytea"
	case "date":
		typ = "pgtype.Date"
	case "timestamp without time zone":
		typ = "pgtype.Timestamp"
	case "timestamp with time zone":
		typ = "pgtype.Timestamptz"
	case "time with time zone", "time without time zone":
		// pgtype uses the same type for both types as postgres' type
		// `time with time zone` does not actually store a time zone.
		// Source: https://github.com/jackc/pgtype/issues/14#issuecomment-559316485
		typ = "pgtype.Time"
	case "interval":
		typ = "pgtype.Interval"
	case `"char"`:
		typ = "pgtype.QChar"
	case "bit":
		typ = "pgtype.Bit"
	case "bit varying", `"any"`:
		typ = "pgtype.Varbit"
	case "hstore":
		typ = "pgtype.Hstore"
	case "uuid":
		typ = "pgtype.UUID"
	default:
		return "", "", false
	}
	return typ, typ + "{Status: pgtype.Null}", true
}

func postgresNameToGoName(dt string, nullable bool, asSlice *bool, int32Type string, uint32Type string) (typ string, nilVal string, ok bool) {
	switch dt {
	case "boolean":
		nilVal = "false"
		typ = "bool"
		if nullable {
			nilVal = "sql.NullBool{}"
			typ = "sql.NullBool"
		}

	case "character", "character varying", "text", "money", "inet":
		nilVal = `""`
		typ = "string"
		if nullable {
			nilVal = "sql.NullString{}"
			typ = "sql.NullString"
		}

	case "smallint":
		nilVal = "0"
		typ = "int16"
		if nullable {
			nilVal = "sql.NullInt64{}"
			typ = "sql.NullInt64"
		}
	case "integer":
		nilVal = "0"
		typ = int32Type
		if nullable {
			nilVal = "sql.NullInt64{}"
			typ = "sql.NullInt64"
		}

	case "bigint":
		nilVal = "0"
		typ = "int64"
		if nullable {
			nilVal = "sql.NullInt64{}"
			typ = "sql.NullInt64"
		}

	case "smallserial":
		nilVal = "0"
		typ = "uint16"
		if nullable {
			nilVal = "sql.NullInt64{}"
			typ = "sql.NullInt64"
		}
	case "serial":
		nilVal = "0"
		typ = uint32Type
		if nullable {
			nilVal = "sql.NullInt64{}"
			typ = "sql.NullInt64"
		}
	case "bigserial":
		nilVal = "0"
		typ = "uint64"
		if nullable {
			nilVal = "sql.NullInt64{}"
			typ = "sql.NullInt64"
		}

	case "real":
		nilVal = "0.0"
		typ = "float32"
		if nullable {
			nilVal = "sql.NullFloat64{}"
			typ = "sql.NullFloat64"
		}
	case "numeric", "double precision":
		nilVal = "0.0"
		typ = "float64"
		if nullable {
			nilVal = "sql.NullFloat64{}"
			typ = "sql.NullFloat64"
		}

	case "bytea":
		*asSlice = true
		typ = "byte"

	case "date", "timestamp with time zone", "time with time zone", "time without time zone", "timestamp without time zone":
		nilVal = "time.Time{}"
		typ = "time.Time"
		if nullable {
			nilVal = "pq.NullTime{}"
			typ = "pq.NullTime"
		}

	case "interval":
		typ = "*time.Duration"

	case `"char"`, "bit":
		// FIXME: this needs to actually be tested ...
		// i think this should be 'rune' but I don't think database/sql
		// supports 'rune' as a type?
		//
		// this is mainly here because postgres's pg_catalog.* meta tables have
		// this as a type.
		//typ = "rune"
		nilVal = `uint8(0)`
		typ = "uint8"

	case `"any"`, "bit varying":
		*asSlice = true
		typ = "byte"

	case "hstore":
		typ = "hstore.Hstore"

	case "uuid":
		nilVal = "uuid.New()"
		typ = "uuid.UUID"
	default:
		return "", "", false
	}
	return typ, nilVal, true
}
