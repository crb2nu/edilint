package edilint

// This file holds the confusable tables used by the character-hygiene check.
//
// The tables are deliberately conservative: they map only code points that are
// visually indistinguishable from an ASCII character in a typical monospace
// font, because those are the ones that survive a human review of a file and
// then break a downstream parser. Runes that merely happen to be non-ASCII are
// reported under the lower-severity charset.nonascii rule instead.

// invisible lists code points that render as nothing (or as whitespace that is
// not U+0020) and therefore cannot be spotted by reading the file.
var invisible = map[rune]string{
	0x00AD: "SOFT HYPHEN",
	0x180E: "MONGOLIAN VOWEL SEPARATOR",
	0x200B: "ZERO WIDTH SPACE",
	0x200C: "ZERO WIDTH NON-JOINER",
	0x200D: "ZERO WIDTH JOINER",
	0x200E: "LEFT-TO-RIGHT MARK",
	0x200F: "RIGHT-TO-LEFT MARK",
	0x202A: "LEFT-TO-RIGHT EMBEDDING",
	0x202B: "RIGHT-TO-LEFT EMBEDDING",
	0x202C: "POP DIRECTIONAL FORMATTING",
	0x202D: "LEFT-TO-RIGHT OVERRIDE",
	0x202E: "RIGHT-TO-LEFT OVERRIDE",
	0x2060: "WORD JOINER",
	0x2066: "LEFT-TO-RIGHT ISOLATE",
	0x2067: "RIGHT-TO-LEFT ISOLATE",
	0x2068: "FIRST STRONG ISOLATE",
	0x2069: "POP DIRECTIONAL ISOLATE",
	0xFEFF: "ZERO WIDTH NO-BREAK SPACE",
}

// confusables maps a non-ASCII code point to the ASCII character it imitates.
var confusables = map[rune]rune{
	// Cyrillic capitals.
	0x0405: 'S', 0x0406: 'I', 0x0408: 'J',
	0x0410: 'A', 0x0412: 'B', 0x0415: 'E', 0x0417: '3',
	0x041A: 'K', 0x041C: 'M', 0x041D: 'H', 0x041E: 'O',
	0x0420: 'P', 0x0421: 'C', 0x0422: 'T', 0x0423: 'Y', 0x0425: 'X',
	0x04AE: 'Y',
	// Cyrillic lowercase.
	0x0430: 'a', 0x0432: 'b', 0x0435: 'e', 0x043A: 'k', 0x043C: 'm',
	0x043E: 'o', 0x0440: 'p', 0x0441: 'c', 0x0443: 'y', 0x0445: 'x',
	0x0455: 's', 0x0456: 'i', 0x0458: 'j',
	0x04BB: 'h', 0x04CF: 'l', 0x0501: 'd', 0x051B: 'q', 0x051D: 'w',

	// Greek capitals.
	0x0391: 'A', 0x0392: 'B', 0x0395: 'E', 0x0396: 'Z', 0x0397: 'H',
	0x0399: 'I', 0x039A: 'K', 0x039C: 'M', 0x039D: 'N', 0x039F: 'O',
	0x03A1: 'P', 0x03A4: 'T', 0x03A5: 'Y', 0x03A7: 'X',
	// Greek lowercase.
	0x03B1: 'a', 0x03BD: 'v', 0x03BF: 'o', 0x03C1: 'p',

	// Latin letters outside ASCII that imitate ASCII ones.
	0x0130: 'I', 0x0131: 'i', 0x0261: 'g', 0x01C0: '|',

	// Spaces.
	0x00A0: ' ', 0x2000: ' ', 0x2001: ' ', 0x2002: ' ', 0x2003: ' ',
	0x2004: ' ', 0x2005: ' ', 0x2006: ' ', 0x2007: ' ', 0x2008: ' ',
	0x2009: ' ', 0x200A: ' ', 0x202F: ' ', 0x205F: ' ', 0x3000: ' ',

	// Dashes and minus signs.
	0x2010: '-', 0x2011: '-', 0x2012: '-', 0x2013: '-', 0x2014: '-',
	0x2015: '-', 0x2212: '-', 0xFE58: '-', 0xFE63: '-',

	// Quotes and apostrophes.
	0x2018: '\'', 0x2019: '\'', 0x201A: ',', 0x201B: '\'',
	0x201C: '"', 0x201D: '"', 0x201E: '"', 0x2032: '\'', 0x2033: '"',
	0x00B4: '\'', 0x02B9: '\'', 0x02BC: '\'', 0x02C8: '\'',

	// Punctuation.
	0x2044: '/', 0x2215: '/', 0x2236: ':', 0xA789: ':', 0x037E: ';',
	0x00B7: '.', 0x2027: '.', 0x00D7: 'x',
}

// fullwidthOffset is the distance between an ASCII code point and its fullwidth
// counterpart in the Halfwidth and Fullwidth Forms block.
const fullwidthOffset = 0xFEE0

// confusableASCII reports the ASCII character that r imitates, if any.
func confusableASCII(r rune) (rune, bool) {
	if a, ok := confusables[r]; ok {
		return a, true
	}
	// U+FF01..U+FF5E are fullwidth forms of ASCII 0x21..0x7E.
	if r >= 0xFF01 && r <= 0xFF5E {
		return r - fullwidthOffset, true
	}
	// U+FF10..U+FF19 (fullwidth digits) are covered by the range above.
	return 0, false
}

// isInvisible reports whether r renders as nothing or as non-U+0020 whitespace.
func isInvisible(r rune) (string, bool) {
	name, ok := invisible[r]
	return name, ok
}
