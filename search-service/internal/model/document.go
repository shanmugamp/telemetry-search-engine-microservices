package model

type Document struct {
	ID             int64  `json:"id"`
	MsgId          string `json:"msg_id"          parquet:"name=MsgId, type=BYTE_ARRAY, convertedtype=UTF8"`
	PartitionId    string `json:"partition_id"    parquet:"name=PartitionId, type=BYTE_ARRAY, convertedtype=UTF8"`
	Timestamp      string `json:"timestamp"       parquet:"name=Timestamp, type=BYTE_ARRAY, convertedtype=UTF8"`
	Hostname       string `json:"hostname"        parquet:"name=Hostname, type=BYTE_ARRAY, convertedtype=UTF8"`
	Priority       int32  `json:"priority"        parquet:"name=Priority, type=INT32"`
	Facility       int32  `json:"facility"        parquet:"name=Facility, type=INT32"`
	FacilityString string `json:"facility_string" parquet:"name=FacilityString, type=BYTE_ARRAY, convertedtype=UTF8"`
	Severity       int32  `json:"severity"        parquet:"name=Severity, type=INT32"`
	SeverityString string `json:"severity_string" parquet:"name=SeverityString, type=BYTE_ARRAY, convertedtype=UTF8"`
	AppName        string `json:"app_name"        parquet:"name=AppName, type=BYTE_ARRAY, convertedtype=UTF8"`
	ProcId         string `json:"proc_id"         parquet:"name=ProcId, type=BYTE_ARRAY, convertedtype=UTF8"`
	Message        string `json:"message"         parquet:"name=Message, type=BYTE_ARRAY, convertedtype=UTF8"`
	MessageRaw     string `json:"message_raw"     parquet:"name=MessageRaw, type=BYTE_ARRAY, convertedtype=UTF8"`
	StructuredData string `json:"structured_data" parquet:"name=StructuredData, type=BYTE_ARRAY, convertedtype=UTF8"`
	Tag            string `json:"tag"             parquet:"name=Tag, type=BYTE_ARRAY, convertedtype=UTF8"`
	Sender         string `json:"sender"          parquet:"name=Sender, type=BYTE_ARRAY, convertedtype=UTF8"`
	Groupings      string `json:"groupings"       parquet:"name=Groupings, type=BYTE_ARRAY, convertedtype=UTF8"`
	Event          string `json:"event"           parquet:"name=Event, type=BYTE_ARRAY, convertedtype=UTF8"`
	EventId        string `json:"event_id"        parquet:"name=EventId, type=BYTE_ARRAY, convertedtype=UTF8"`
	NanoTimeStamp  int64  `json:"nano_timestamp"  parquet:"name=NanoTimeStamp, type=INT64"`
	Namespace      string `json:"namespace"       parquet:"name=Namespace, type=BYTE_ARRAY, convertedtype=UTF8"`
}
