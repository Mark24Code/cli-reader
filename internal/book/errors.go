package book

import (
	"errors"
	"fmt"
)

// ErrDRM marks books protected by a DRM scheme. The reader never attempts to
// remove or bypass DRM.
var ErrDRM = errors.New("DRM 保护")

// ErrUnsupportedFormat marks input whose format cannot be determined or is not
// supported.
var ErrUnsupportedFormat = errors.New("不支持的书籍格式")

// FormatError adds the detected format and path to a loader failure.
type FormatError struct {
	Format Format
	Path   string
	Err    error
}

func (e *FormatError) Error() string {
	if e.Format == "" {
		return fmt.Sprintf("%s: %v", e.Path, e.Err)
	}
	return fmt.Sprintf("%s（%s）: %v", e.Path, e.Format, e.Err)
}

func (e *FormatError) Unwrap() error { return e.Err }

// IsDRM reports whether err indicates DRM-protected content.
func IsDRM(err error) bool { return errors.Is(err, ErrDRM) }
