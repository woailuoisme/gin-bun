package enums_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"bgin/internal/shared/enums"
)

func TestOTPType(t *testing.T) {
	t.Parallel()

	values := enums.OTPTypeValues()
	assert.Len(t, values, 3)
	assert.Contains(t, values, enums.OTPTypeSignup)
	assert.Contains(t, values, enums.OTPTypePasswordReset)
	assert.Contains(t, values, enums.OTPTypeEmailVerification)

	for _, v := range values {
		assert.True(t, v.IsValid())
		assert.Equal(t, string(v), v.String())
	}

	invalid := enums.OTPType("invalid_type")
	assert.False(t, invalid.IsValid())
}

func TestDisplayConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "primary", enums.ColorPrimary)
	assert.Equal(t, "secondary", enums.ColorSecondary)
	assert.Equal(t, "success", enums.ColorSuccess)
	assert.Equal(t, "danger", enums.ColorDanger)
	assert.Equal(t, "warning", enums.ColorWarning)
	assert.Equal(t, "info", enums.ColorInfo)
	assert.Equal(t, "gray", enums.ColorGray)
	assert.Equal(t, "purple", enums.ColorPurple)
	assert.Equal(t, "orange", enums.ColorOrange)
	assert.Equal(t, "Pending", enums.LabelPending)
}
