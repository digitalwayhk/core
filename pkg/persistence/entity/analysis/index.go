package analysis

type IndexColumn struct {
	ColumnName string `json:"columnName"`
	IndexType  string `json:"indexType"` //count,sum,avg,min,max,distinct
}
