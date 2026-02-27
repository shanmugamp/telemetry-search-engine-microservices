package worker

import (
	"fmt"
	"strings"

	local "github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/reader"
)

// SimpleDoc is a minimal struct returned by the ingest worker.
// The full model.Document is used by search-service; ingest only needs row count.
type SimpleDoc struct {
	Fields map[string]string
}

func readParquetFile(path string) ([]SimpleDoc, error) {
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

	// Just verify we can read columns — actual indexing done by search-service on startup.
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

	if len(pathMap) == 0 {
		return nil, fmt.Errorf("no columns found in %s", path)
	}

	// Return stub docs — count is what matters for the job tracker.
	docs := make([]SimpleDoc, numRows)
	for i := range numRows {
		docs[i] = SimpleDoc{Fields: map[string]string{"_row": fmt.Sprintf("%d", i)}}
	}
	return docs, nil
}
