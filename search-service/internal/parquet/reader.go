package parquet

import (
	"fmt"
	"log/slog"
	"strings"

	"search-service/internal/model"

	local "github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/reader"
)

// ReadParquet reads all rows from a Parquet file using schema discovery.
func ReadParquet(path string) ([]model.Document, error) {
	fr, err := local.NewLocalFileReader(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer fr.Close()

	pr, err := reader.NewParquetColumnReader(fr, 4)
	if err != nil {
		return nil, fmt.Errorf("parquet reader: %w", err)
	}
	defer pr.ReadStop()

	numRows := int(pr.GetNumRows())
	if numRows == 0 {
		return nil, nil
	}

	// Discover columns from schema.
	pathMap := make(map[string]string)
	for _, pathKey := range pr.SchemaHandler.ValueColumns {
		var leafName string
		if idx := strings.IndexByte(pathKey, '\x01'); idx >= 0 {
			leafName = pathKey[idx+1:]
		} else {
			leafName = pathKey
		}
		pathMap[strings.ToLower(leafName)] = pathKey
	}

	slog.Debug("parquet discovered columns", "count", len(pathMap))

	colData := make(map[string][]string, len(pathMap))
	for lcName, fullPath := range pathMap {
		vals, _, _, err := pr.ReadColumnByPath(fullPath, int64(numRows))
		if err != nil {
			slog.Warn("skipping unreadable column", "col", lcName, "err", err)
			continue
		}
		strs := make([]string, numRows)
		for i, v := range vals {
			if i >= numRows {
				break
			}
			switch t := v.(type) {
			case string:
				strs[i] = t
			case []byte:
				strs[i] = string(t)
			default:
				strs[i] = fmt.Sprintf("%v", t)
			}
		}
		colData[lcName] = strs
	}

	if len(colData) == 0 {
		return nil, fmt.Errorf("no readable columns found in %s", path)
	}

	docs := make([]model.Document, numRows)
	for i := range numRows {
		docs[i] = model.Document{
			MsgId:          get(colData, i, "msgid", "msg_id"),
			PartitionId:    get(colData, i, "partitionid", "partition_id"),
			Timestamp:      get(colData, i, "timestamp"),
			Hostname:       get(colData, i, "hostname"),
			FacilityString: get(colData, i, "facilitystring", "facility_string"),
			SeverityString: get(colData, i, "severitystring", "severity_string"),
			AppName:        get(colData, i, "appname", "app_name"),
			ProcId:         get(colData, i, "procid", "proc_id"),
			Message:        get(colData, i, "message"),
			MessageRaw:     get(colData, i, "messageraw", "message_raw"),
			StructuredData: get(colData, i, "structureddata", "structured_data"),
			Tag:            get(colData, i, "tag"),
			Sender:         get(colData, i, "sender"),
			Groupings:      get(colData, i, "groupings"),
			Event:          get(colData, i, "event"),
			EventId:        get(colData, i, "eventid", "event_id"),
			Namespace:      get(colData, i, "namespace"),
		}
		docs[i].NanoTimeStamp = parseInt64(get(colData, i, "nanotimestamp", "nano_timestamp"))
		docs[i].Priority = int32(parseInt64(get(colData, i, "priority")))
		docs[i].Facility = int32(parseInt64(get(colData, i, "facility")))
		docs[i].Severity = int32(parseInt64(get(colData, i, "severity")))
	}

	return docs, nil
}

func get(data map[string][]string, i int, names ...string) string {
	for _, name := range names {
		if col, ok := data[name]; ok && i < len(col) {
			return col[i]
		}
	}
	return ""
}

func parseInt64(s string) int64 {
	var n int64
	fmt.Sscanf(s, "%d", &n)
	return n
}
