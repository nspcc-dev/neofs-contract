package container

type (
	// CORSRule describes one rule for the container’s CORS attribute.
	CORSRule struct {
		AllowedMethods []string
		AllowedOrigins []string
		AllowedHeaders []string
		ExposeHeaders  []string
		MaxAgeSeconds  int64
	}
)
