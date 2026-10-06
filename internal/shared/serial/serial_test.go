package serial

import (
	"regexp"
	"testing"
)

func TestGenerateUsesLongRandomSuffix(t *testing.T) {
	got := Generate("DJ")
	if matched := regexp.MustCompile(`^DJ[0-9]{24}$`).MatchString(got); !matched {
		t.Fatalf("unexpected serial format: %q", got)
	}
}
