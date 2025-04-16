package libsql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

// Document represents a single document.
type Document struct {
	ID        string
	Metadata  map[string]string
	Embedding []float64
	Content   string
}

// Result represents a single result from a query.
type Result struct {
	ID        string
	Metadata  map[string]string
	Embedding []float64
	Content   string

	// The cosine similarity between the query and the document.
	// The higher the value, the more similar the document is to the query.
	// The value is in the range [-1, 1].
	Similarity float32
}

type LibSqlDb struct {
	tableName string
	client    *sql.DB
	ctx       context.Context
}

func InitLibSqlDb(ctx context.Context, path, tableName string) (*LibSqlDb, error) {
	if len(tableName) == 0 {
		tableName = "vectors"
	}

	db, err := sql.Open("libsql", fmt.Sprintf("file:%s", path))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open db %s: %s", path, err)
		return nil, err
	}

	sqldb := &LibSqlDb{
		tableName: tableName,
		client:    db,
		ctx:       ctx,
	}
	return sqldb, nil
}

func (sqldb *LibSqlDb) Init(dimensions int) error {
	_, err := sqldb.client.ExecContext(sqldb.ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
            id              TEXT PRIMARY KEY,
            pageContent     TEXT UNIQUE,
            source          TEXT NOT NULL,
            vector          F32_BLOB(%d),
            metadata        TEXT
        );`, sqldb.tableName, dimensions))

	return err
}

func (sqldb *LibSqlDb) InsertChunk(doc *Document) error {
	embeddings := ""
	for _, v := range doc.Embedding {
		if len(embeddings) > 0 {
			embeddings = fmt.Sprintf("%s,%f", embeddings, v)
		} else {
			embeddings = fmt.Sprintf("%f", v)
		}
	}

	byteMeta, err := json.Marshal(doc.Metadata)
	if err != nil {
		return err
	}
	stmt := fmt.Sprintf(`INSERT OR IGNORE INTO %s (id, pageContent, vector, metadata)
            VALUES (?, ?, vector32('[%s]'), ?);`, sqldb.tableName, embeddings)
	_, err = sqldb.client.ExecContext(sqldb.ctx, stmt, doc.ID, doc.Content, string(byteMeta))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to execute statement %s: %s", stmt, err)
		return err
	}
	return nil
}

func (sqldb *LibSqlDb) DeleteKeys(docId string) error {
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE
	   id = '%s';`, sqldb.tableName, docId)
	_, err := sqldb.client.ExecContext(sqldb.ctx, stmt)
	return err
}

func (sqldb *LibSqlDb) GetVectorCount() (int, error) {
	stmt := fmt.Sprintf(`SELECT count(id) as count FROM %s;`, sqldb.tableName)
	query, err := sqldb.client.QueryContext(sqldb.ctx, stmt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to execute query %s: %s", stmt, err)
		os.Exit(1)
	}
	for query.Next() {
		var v int
		if err := query.Scan(&v); err != nil {
			fmt.Fprintf(os.Stderr, "failed to scan row: %s", err)
			return 0, err
		}

		return v, nil
	}

	if err := query.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "errors from query: %s", err)
		return 0, err
	}
	return 0, nil
}

func (sqldb *LibSqlDb) Reset() error {
	stmt := fmt.Sprintf(`DELETE FROM %s;`, sqldb.tableName)
	_, err := sqldb.client.ExecContext(sqldb.ctx, stmt)
	return err
}

func (sqldb *LibSqlDb) similaritySearch(queryEmbedding []float64, TopK int64) ([]*Result, error) {
	const statement = `SELECT id, pageContent, metadata,
	vector_distance_cos(vector, vector32('[%s]')) as distance
FROM %s
ORDER BY vector_distance_cos(vector, vector32('[%s]')) ASC
LIMIT %d;`

	embeddings := ""
	for _, v := range queryEmbedding {
		if len(embeddings) > 0 {
			embeddings = fmt.Sprintf("%s,%f", embeddings, v)
		} else {
			embeddings = fmt.Sprintf("%f", v)
		}
	}

	stmt := fmt.Sprintf(statement, embeddings, sqldb.tableName, embeddings, TopK)

	query, err := sqldb.client.QueryContext(sqldb.ctx, stmt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to execute query %s: %s", stmt, err)
		return nil, err
	}

	datas := make([]*Result, 0)
	for query.Next() {
		result := &Result{}
		meta := ""
		if err := query.Scan(&result.ID, &result.Content, &meta, &result.Similarity); err != nil {
			fmt.Fprintf(os.Stderr, "failed to scan row: %s", err)
			return nil, err
		}
		err = json.Unmarshal([]byte(meta), result.Metadata)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to execute query %s: %s", stmt, err)
			return nil, err
		}
		result.Similarity = 1 - result.Similarity
		datas = append(datas, result)
	}

	if err := query.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "errors from query: %s", err)
		return datas, err
	}
	return datas, nil

}
