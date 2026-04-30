package version

// Version is the canonical runtime version for every operator-facing surface.
// Release-shaped builds stamp this with:
//
//	-X roboticus/internal/version.Version=<version>
//
// Local source builds intentionally report "dev".
var Version = "dev"
