package archive

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/parquet-go/parquet-go"
)

type Column struct {
	Name string
	Kind Kind
}

type Kind int

const (
	KindString Kind = iota
	KindInt
	KindDouble
	KindBool
	KindBytes
)

func KindOf(databaseType string) Kind {
	switch databaseType {
	case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "INTEGER", "BIGINT", "YEAR":
		return KindInt
	case "FLOAT", "DOUBLE", "REAL":
		return KindDouble
	case "BIT", "BOOLEAN", "BOOL":
		return KindBool
	case "BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB", "BINARY", "VARBINARY":
		return KindBytes
	default:
		return KindString
	}
}

// WriteParquet converts rows into a parquet file on w. The writer is closed
// before return, which flushes the footer. Callers can pass an io.Pipe so the
// file never lands on disk.
func WriteParquet(w io.Writer, columns []Column, rows func() ([]any, error)) (int64, error) {
	if len(columns) == 0 {
		return 0, fmt.Errorf("the table has no columns")
	}
	group := make(parquet.Group, len(columns))
	for _, column := range columns {
		group[column.Name] = optionalNode(column.Kind)
	}
	schema := parquet.NewSchema("archive", group)
	writer := parquet.NewWriter(w, schema, parquet.MaxRowsPerRowGroup(8192))
	order := make([]string, len(schema.Fields()))
	for i, field := range schema.Fields() {
		order[i] = field.Name()
	}
	index := map[string]int{}
	kinds := map[string]Kind{}
	for i, column := range columns {
		index[column.Name] = i
		kinds[column.Name] = column.Kind
	}
	var count int64
	batch := make([]parquet.Row, 0, 256)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if _, err := writer.WriteRows(batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}
	for {
		raw, err := rows()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = writer.Close()
			return count, err
		}
		row := make(parquet.Row, len(order))
		for leaf, name := range order {
			kind := kinds[name]
			var value any
			if src, ok := index[name]; ok && src < len(raw) {
				value = raw[src]
			}
			row[leaf] = parquetValue(kind, value).Level(0, definition(value), leaf)
		}
		batch = append(batch, row)
		count++
		if len(batch) == cap(batch) {
			if err := flush(); err != nil {
				_ = writer.Close()
				return count, err
			}
		}
	}
	if err := flush(); err != nil {
		_ = writer.Close()
		return count, err
	}
	if err := writer.Close(); err != nil {
		return count, err
	}
	return count, nil
}

func optionalNode(kind Kind) parquet.Node {
	switch kind {
	case KindInt:
		return parquet.Optional(parquet.Int(64))
	case KindDouble:
		return parquet.Optional(parquet.Leaf(parquet.DoubleType))
	case KindBool:
		return parquet.Optional(parquet.Leaf(parquet.BooleanType))
	default:
		return parquet.Optional(parquet.String())
	}
}

func definition(value any) int {
	if value == nil {
		return 0
	}
	return 1
}

func parquetValue(kind Kind, value any) parquet.Value {
	if value == nil {
		return parquet.NullValue()
	}
	switch kind {
	case KindInt:
		n, ok := asInt(value)
		if !ok {
			return parquet.ByteArrayValue([]byte(fmt.Sprint(value)))
		}
		return parquet.Int64Value(n)
	case KindDouble:
		n, ok := asFloat(value)
		if !ok {
			return parquet.DoubleValue(0)
		}
		return parquet.DoubleValue(n)
	case KindBool:
		return parquet.BooleanValue(asBool(value))
	case KindBytes:
		return parquet.ByteArrayValue(asBytes(value))
	default:
		return parquet.ByteArrayValue([]byte(asString(value)))
	}
}

func asInt(value any) (int64, bool) {
	switch n := value.(type) {
	case int64:
		return n, true
	case int32:
		return int64(n), true
	case int:
		return int64(n), true
	case []byte:
		parsed, err := strconv.ParseInt(string(n), 10, 64)
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseInt(n, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func asFloat(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case []byte:
		parsed, err := strconv.ParseFloat(string(n), 64)
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(n, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func asBool(value any) bool {
	switch n := value.(type) {
	case bool:
		return n
	case int64:
		return n != 0
	case []byte:
		return string(n) == "1" || string(n) == "true"
	default:
		return false
	}
}

func asBytes(value any) []byte {
	switch n := value.(type) {
	case []byte:
		return append([]byte(nil), n...)
	default:
		return []byte(asString(value))
	}
}

func asString(value any) string {
	switch n := value.(type) {
	case string:
		return n
	case []byte:
		return string(n)
	case time.Time:
		return n.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(n)
	}
}
