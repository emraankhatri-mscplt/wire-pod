package wirepod_ttr

// A small bitmap font for sight words practice.
//
// Vector's screen is only 184x96 pixels, so words are drawn from blocky
// 5x7 glyphs which are then scaled up to fill the screen. Keeping the font
// here means no font files have to be shipped and no font rendering library
// has to be pulled in.
//
// Each glyph is seven rows of five columns, separated by '/'. A '#' is a lit
// pixel. Only the characters sanitizeSightWords allows need a glyph.

const (
	sightWordGlyphWidth  = 5
	sightWordGlyphHeight = 7
	// blank column between two letters
	sightWordGlyphSpacing = 1
)

var sightWordGlyphs = map[rune]string{
	'a':  "...../...../.###./....#/.####/#...#/.####",
	'b':  "#..../#..../####./#...#/#...#/#...#/####.",
	'c':  "...../...../.###./#..../#..../#...#/.###.",
	'd':  "....#/....#/.####/#...#/#...#/#...#/.####",
	'e':  "...../...../.###./#...#/#####/#..../.###.",
	'f':  "..##./.#.../####./.#.../.#.../.#.../.#...",
	'g':  "...../.####/#...#/#...#/.####/....#/.###.",
	'h':  "#..../#..../####./#...#/#...#/#...#/#...#",
	'i':  "..#../...../.##../..#../..#../..#../.###.",
	'j':  "...#./...../..##./...#./...#./#..#./.##..",
	'k':  "#..../#..../#..#./#.#../##.../#.#../#..#.",
	'l':  ".##../..#../..#../..#../..#../..#../.###.",
	'm':  "...../...../##.#./#.#.#/#.#.#/#.#.#/#.#.#",
	'n':  "...../...../####./#...#/#...#/#...#/#...#",
	'o':  "...../...../.###./#...#/#...#/#...#/.###.",
	'p':  "...../####./#...#/#...#/####./#..../#....",
	'q':  "...../.####/#...#/#...#/.####/....#/....#",
	'r':  "...../...../#.##./##..#/#..../#..../#....",
	's':  "...../...../.####/#..../.###./....#/####.",
	't':  ".#.../.#.../####./.#.../.#.../.#..#/..##.",
	'u':  "...../...../#...#/#...#/#...#/#..##/.##.#",
	'v':  "...../...../#...#/#...#/#...#/.#.#./..#..",
	'w':  "...../...../#...#/#...#/#.#.#/#.#.#/.#.#.",
	'x':  "...../...../#...#/.#.#./..#../.#.#./#...#",
	'y':  "...../#...#/#...#/#...#/.####/....#/.###.",
	'z':  "...../...../#####/...#./..#../.#.../#####",
	'A':  ".###./#...#/#...#/#####/#...#/#...#/#...#",
	'B':  "####./#...#/#...#/####./#...#/#...#/####.",
	'C':  ".###./#...#/#..../#..../#..../#...#/.###.",
	'D':  "####./#...#/#...#/#...#/#...#/#...#/####.",
	'E':  "#####/#..../#..../####./#..../#..../#####",
	'F':  "#####/#..../#..../####./#..../#..../#....",
	'G':  ".###./#...#/#..../#.###/#...#/#...#/.###.",
	'H':  "#...#/#...#/#...#/#####/#...#/#...#/#...#",
	'I':  ".###./..#../..#../..#../..#../..#../.###.",
	'J':  "..###/...#./...#./...#./...#./#..#./.##..",
	'K':  "#...#/#..#./#.#../##.../#.#../#..#./#...#",
	'L':  "#..../#..../#..../#..../#..../#..../#####",
	'M':  "#...#/##.##/#.#.#/#...#/#...#/#...#/#...#",
	'N':  "#...#/##..#/#.#.#/#..##/#...#/#...#/#...#",
	'O':  ".###./#...#/#...#/#...#/#...#/#...#/.###.",
	'P':  "####./#...#/#...#/####./#..../#..../#....",
	'Q':  ".###./#...#/#...#/#...#/#.#.#/#..#./.##.#",
	'R':  "####./#...#/#...#/####./#.#../#..#./#...#",
	'S':  ".####/#..../#..../.###./....#/....#/####.",
	'T':  "#####/..#../..#../..#../..#../..#../..#..",
	'U':  "#...#/#...#/#...#/#...#/#...#/#...#/.###.",
	'V':  "#...#/#...#/#...#/#...#/#...#/.#.#./..#..",
	'W':  "#...#/#...#/#...#/#...#/#.#.#/##.##/#...#",
	'X':  "#...#/#...#/.#.#./..#../.#.#./#...#/#...#",
	'Y':  "#...#/#...#/.#.#./..#../..#../..#../..#..",
	'Z':  "#####/....#/...#./..#../.#.../#..../#####",
	'\'': "..#../..#../...../...../...../...../.....",
	'-':  "...../...../...../#####/...../...../.....",
}

// sightWordGlyph returns the glyph for a character. Characters without a
// glyph, which sanitizeSightWords should already have filtered out, are drawn
// as a blank space rather than dropped, so a word never silently loses a
// letter without it being visible.
func sightWordGlyph(r rune) (string, bool) {
	if glyph, ok := sightWordGlyphs[r]; ok {
		return glyph, true
	}
	// the curly apostrophe is common in copied word lists
	if r == '’' {
		return sightWordGlyphs['\''], true
	}
	return "", false
}

// sightWordGlyphLit reports whether the pixel at x,y of a glyph is lit.
func sightWordGlyphLit(glyph string, x, y int) bool {
	if x < 0 || x >= sightWordGlyphWidth || y < 0 || y >= sightWordGlyphHeight {
		return false
	}
	// each row is sightWordGlyphWidth characters plus the '/' separator
	index := y*(sightWordGlyphWidth+1) + x
	if index >= len(glyph) {
		return false
	}
	return glyph[index] == '#'
}
