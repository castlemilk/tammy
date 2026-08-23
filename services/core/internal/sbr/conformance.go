package sbr

// Conformance is the authenticated signed-registration stage carried by a runtime profile.
type Conformance string

const (
	ConformanceSimulator Conformance = "SIMULATOR"
	ConformancePre       Conformance = "PRE_CONFORMANCE"
	ConformancePost      Conformance = "POST_CONFORMANCE"
)
