package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripNonBMP_StripsEmoji(t *testing.T) {
	assert.Equal(t, " Ruth", StripNonBMP("🦋 Ruth"))
	assert.Equal(t, "Ruth", StripNonBMP("Ruth🦋"))
	// Only the astral char is removed; surrounding spaces are preserved.
	assert.Equal(t, "Ana  Maria", StripNonBMP("Ana 🦋 Maria"))
}

func TestStripNonBMP_StripsMultipleAstralChars(t *testing.T) {
	assert.Equal(t, "AB", StripNonBMP("A🦋😀B👍"))
}

func TestStripNonBMP_PreservesBMPText(t *testing.T) {
	// Plain ASCII, Hebrew, Cyrillic, accented Latin and BMP CJK must be untouched.
	assert.Equal(t, "Test User", StripNonBMP("Test User"))
	assert.Equal(t, "משה כהן", StripNonBMP("משה כהן"))
	assert.Equal(t, "Владимир", StripNonBMP("Владимир"))
	assert.Equal(t, "José Muñoz", StripNonBMP("José Muñoz"))
	assert.Equal(t, "山田太郎", StripNonBMP("山田太郎"))
}

func TestStripNonBMP_EmptyString(t *testing.T) {
	assert.Equal(t, "", StripNonBMP(""))
}

func TestStripNonBMP_ReturnsInputWhenNothingToStrip(t *testing.T) {
	in := "no astral chars here"
	assert.Equal(t, in, StripNonBMP(in))
}
