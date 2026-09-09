package embed

// Compile-time conformance: concrete clients satisfy the capability interfaces.
var (
	_ QueryEmbedder    = (*Client)(nil)
	_ DocumentEmbedder = (*Client)(nil)
	_ BatchEmbedder    = (*Client)(nil)
	_ VLQueryEmbedder  = (*VLClient)(nil)
	_ VLTextBatcher    = (*VLClient)(nil)
	_ VLImageBatcher   = (*VLClient)(nil)
)
