package cjsmodulelexer

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

var source string
var pos int
var end int
var openTokenDepth int
var templateDepth int
var lastTokenPos int
var lastSlashWasDivision bool
var templateStack []int
var templateStackDepth int
var openTokenPosStack []int
var openClassPosStack []bool
var nextBraceIsClass bool
var starExportMap map[string]sql.NullString
var lastStarExportSpecifier sql.NullString
var exports map[sql.NullString]struct{}
var unsafeGetters map[string]struct{}
var reexports map[sql.NullString]struct{}

func resetState() {
	openTokenDepth = 0
	templateDepth = -1
	lastTokenPos = -1
	lastSlashWasDivision = false
	templateStack = make([]int, 1024)
	templateStackDepth = 0
	openTokenPosStack = make([]int, 1024)
	openClassPosStack = make([]bool, 1024)
	nextBraceIsClass = false
	starExportMap = map[string]sql.NullString{}
	lastStarExportSpecifier = sql.NullString{}

	exports = map[sql.NullString]struct{}{}
	unsafeGetters = map[string]struct{}{}
	reexports = map[sql.NullString]struct{}{}
}

const import2 = 0
const exportAssign = 1
const exportStar = 2

type Exports struct {
	Exports   []string
	Reexports []string
}

func parseCJS(source string, name sql.NullString) (Exports, error) {
	resetState()
	_, err := parseSource(source)
	if err != nil {
		return Exports{}, fmt.Errorf("%w\nloc=%d\n at %s:%d:%d", err, pos, name, 0, 0)
	}
	exportsSlice := []string{}
	for k := range exports {
		if k.Valid {
			if _, ok := unsafeGetters[k.String]; !ok {
				exportsSlice = append(exportsSlice, k.String)
			}
		}
	}
	reexportsSlice := []string{}
	for k := range reexports {
		if k.Valid {
			reexportsSlice = append(reexportsSlice, k.String)
		}
	}
	result := Exports{
		Exports:   exportsSlice,
		Reexports: reexportsSlice,
	}
	resetState()
	return result, nil
}

func decode(str string) string {
	if str[0] == '"' || str[0] == '\'' {
		decoded, err := strconv.Unquote(str)
		if err != nil {
			panic(err)
		}
		return decoded
	} else {
		return str
	}
}

func parseSource(cjsSource string) (sql.NullBool, error) {
	source = cjsSource
	pos = -1
	end = len(source) - 1
	ch := byte(0)

	if source[0] == '#' && source[1] == '!' {
		if len(source) == 2 {
			return sql.NullBool{Bool: true, Valid: true}, nil
		}
		pos += 2
		for pos++; pos < end; pos++ {
			ch = source[pos]
			if ch == '\n' || ch == '\r' {
				break
			}
		}
	}

	for pos++; pos < end; pos++ {
		ch = source[pos]

		if ch == 32 || ch < 14 && ch > 8 {
			continue
		}

		if openTokenDepth == 0 {
			switch ch {
			case 'i':
				if strings.HasPrefix(source[pos+1:], "mport") && keywordStart(pos) {
					err := throwIfImportStatement()
					if err != nil {
						return sql.NullBool{}, err
					}
				}
				lastTokenPos = pos
				continue
			case 'r':
				startPos := pos
				if tryParseRequire(import2) && keywordStart(startPos) {
					tryBacktrackAddStarExportBinding(startPos - 1)
				}
				lastTokenPos = pos
				continue
			case '_':
				if strings.HasPrefix(source[pos+1:], "interopRequireWildcard") && (keywordStart(pos) || source[pos-1] == '.') {
					startPos := pos
					pos += 23
					if source[pos] == '(' {
						pos++
						openTokenPosStack[openTokenDepth] = lastTokenPos
						openTokenDepth++
						if tryParseRequire(import2) && keywordStart(startPos) {
							tryBacktrackAddStarExportBinding(startPos - 1)
						}
					}
				} else if strings.HasPrefix(source[pos+1:], "_export") && (keywordStart(pos) || source[pos-1] == '.') {
					pos += 8
					if strings.HasPrefix(source[pos:], "Star") {
						pos += 4
					}
					if source[pos] == '(' {
						openTokenPosStack[openTokenDepth] = lastTokenPos
						openTokenDepth++
						pos++
						if source[pos] == 'r' {
							tryParseRequire(exportStar)
						}
					}
				}
				lastTokenPos = pos
				continue
			}
		}

		switch ch {
		case 'e':
			if strings.HasPrefix(source[pos+1:], "xport") && keywordStart(pos) {
				if source[pos+6] == 's' {
					tryParseExportsDotAssign(false)
				} else if openTokenDepth == 0 {
					err := throwIfExportStatement()
					if err != nil {
						return sql.NullBool{}, err
					}
				}
			}
		case 'c':
			if keywordStart(pos) && strings.HasPrefix(source[pos+1:], "lass") && isBrOrWs(source[pos+5]) {
				nextBraceIsClass = true
			}
		case 'm':
			if strings.HasPrefix(source[pos+1:], "odule") && keywordStart(pos) {
				tryParseModuleExportsDotAssign()
			}
		case 'O':
			if strings.HasPrefix(source[pos+1:], "bject") && keywordStart(pos) {
				tryParseObjectDefineOrKeys(openTokenDepth == 0)
			}
		case '(':
			openTokenPosStack[openTokenDepth] = lastTokenPos
			openTokenDepth++
		case ')':
			if openTokenDepth == 0 {
				return sql.NullBool{}, fmt.Errorf("unexpected closing parenthesis")
			}
			openTokenDepth--
		case '{':
			openClassPosStack[openTokenDepth] = nextBraceIsClass
			nextBraceIsClass = false
			openTokenPosStack[openTokenDepth] = lastTokenPos
			openTokenDepth++
		case '}':
			if openTokenDepth == 0 {
				return sql.NullBool{}, fmt.Errorf("unexpected closing brace")
			}
			openTokenDepth2 := openTokenDepth
			openTokenDepth--
			if openTokenDepth2 == templateDepth {
				templateStackDepth--
				templateDepth = templateStack[templateStackDepth]
				templateString()
			} else {
				if templateDepth != -1 && openTokenDepth < templateDepth {
					return sql.NullBool{}, fmt.Errorf("unexpected closing brace")
				}
			}
		case '>':
		case '\'':
			fallthrough
		case '"':
			stringLiteral()
		case '/':
			nextCh := source[pos+1]
			if nextCh == '/' {
				lineComment()
				continue
			} else if nextCh == '*' {
				blockComment()
				continue
			} else {
				lastToken := source[lastTokenPos]
				if isExpressionPunctuator(lastToken) && !(lastToken == '.' && (source[lastTokenPos-1] >= '0' && source[lastTokenPos-1] <= '9')) && !(lastToken == '+' && source[lastTokenPos-1] == '+') && !(lastToken == '-' && source[lastTokenPos-1] == '-') || lastToken == ')' && isParenKeyword(openTokenPosStack[openTokenDepth]) || lastToken == '}' && (isExpressionTerminator(openTokenPosStack[openTokenDepth]) || openClassPosStack[openTokenDepth]) || lastToken == '/' && lastSlashWasDivision || isExpressionKeyword(lastTokenPos) || !lastToken {
					regularExpression()
					lastSlashWasDivision = false
				} else {
					lastSlashWasDivision = true
				}
			}
		case '`':
			templateString()
		}
		lastTokenPos = pos
	}

	if templateDepth != -1 {
		return sql.NullBool{}, fmt.Errorf("unterminated template")
	}

	if openTokenDepth != 0 {
		return sql.NullBool{}, fmt.Errorf("unterminated braces")
	}

	return sql.NullBool{}, nil
}

func tryBacktrackAddStarExportBinding(bPos int) {
	for source[bPos] == ' ' && bPos >= 0 {
		bPos--
	}
	if source[bPos] == '=' {
		bPos--
		for source[bPos] == ' ' && bPos >= 0 {
			bPos--
		}
		var codePoint rune
		idEnd := bPos
		identifierStart := false
		for codePoint = codePointAtLast(bPos); codePoint != 0 && bPos >= 0; codePoint = codePointAtLast(bPos) {
			if codePoint == '\\' {
				return
			}
			if !isIdentifierChar(codePoint, true) {
				break
			}
			identifierStart = isIdentifierStart(codePoint, true)
			bPos -= codePointLen(codePoint)
		}
		if identifierStart && source[bPos] == ' ' {
			starExportId := source[bPos+1 : idEnd+1]
			for source[bPos] == ' ' && bPos >= 0 {
				bPos--
			}
			switch source[bPos] {
			case 'r':
				if !strings.HasPrefix(source[bPos-2:], "va") {
					return
				}
			case 't':
				if !strings.HasPrefix(source[bPos-2:], "le") && !strings.HasPrefix(source[bPos-4:], "cons") {
					return
				}
			default:
				return
			}
			starExportMap[starExportId] = lastStarExportSpecifier
		}
	}
}

func tryParseObjectHasOwnProperty(itId string) bool {
	ch := commentWhitespace()
	if ch != 'O' || !strings.HasPrefix(source[pos+1:], "bject") {
		return false
	}
	pos += 6
	ch = commentWhitespace()
	if ch != '.' {
		return false
	}
	pos++
	ch = commentWhitespace()
	if ch == 'p' {
		if !strings.HasPrefix(source[pos+1:], "rototype") {
			return false
		}
		pos += 9
		ch = commentWhitespace()
		if ch != '.' {
			return false
		}
		pos++
		ch = commentWhitespace()
	}
	if ch != 'h' || !strings.HasPrefix(source[pos+1:], "asOwnProperty") {
		return false
	}
	pos += 14
	ch = commentWhitespace()
	if ch != '.' {
		return false
	}
	pos++
	ch = commentWhitespace()
	if ch != 'c' || !strings.HasPrefix(source[pos+1:], "all") {
		return false
	}
	pos += 4
	ch = commentWhitespace()
	if ch != '(' {
		return false
	}
	pos++
	ch = commentWhitespace()
	if !identifier() {
		return false
	}
	ch = commentWhitespace()
	if ch != ',' {
		return false
	}
	pos++
	ch = commentWhitespace()
	if !strings.HasPrefix(source[pos], itId) {
		return false
	}
	pos += len(itId)
	ch = commentWhitespace()
	if ch != ')' {
		return false
	}
	pos++
	return true
}

func tryParseObjectDefineOrKeys(keys any) {
	pos += 6
	revertPos := pos - 1
	ch := commentWhitespace()
	if ch == '.' {
		pos++
		ch = commentWhitespace()
		if ch == 'd' && strings.HasPrefix(source[pos+1:], "efineProperty") {
			var expt string
			for {
				pos += 14
				revertPos = pos - 1
				ch = commentWhitespace()
				if ch != '(' {
					break
				}
				pos++
				ch = commentWhitespace()
				if !readExportsOrModuleDotExports(ch) {
					break
				}
				ch = commentWhitespace()
				if ch != ',' {
					break
				}
				pos++
				ch = commentWhitespace()
				if ch != '\'' && ch != '"' {
					break
				}
				exportPos := pos
				stringLiteral(ch)
				pos++
				expt = source[exportPos:pos]
				ch = commentWhitespace()
				if ch != ',' {
					break
				}
				pos++
				ch = commentWhitespace()
				if ch != '{' {
					break
				}
				pos++
				ch = commentWhitespace()
				if ch == 'e' {
					if !strings.HasPrefix(source[pos+1:], "numerable") {
						break
					}
					pos += 10
					ch = commentWhitespace()
					if ch != ':' {
						break
					}
					pos++
					ch = commentWhitespace()
					if ch != 't' && !strings.HasPrefix(source[pos+1:], "rue") {
						break
					}
					pos += 4
					ch = commentWhitespace()
					if ch != 44 {
						break
					}
					pos++
					ch = commentWhitespace()
				}
				if ch == 'v' {
					if !strings.HasPrefix(source[pos+1:], "alue") {
						break
					}
					pos += 5
					ch = commentWhitespace()
					if ch != ':' {
						break
					}
					exports[decode(expt)]
					pos = revertPos
					return
				} else if ch == 'g' {
					if !strings.HasPrefix(source[pos+1:], "et") {
						break
					}
					pos += 3
					ch = commentWhitespace()
					if ch == ':' {
						pos++
						ch = commentWhitespace()
						if ch != 'f' {
							break
						}
						if !strings.HasPrefix(source[pos+1:], "unction") {
							break
						}
						pos += 8
						lastPos := pos
						ch = commentWhitespace()
						if ch != 40 && (lastPos == pos || !identifier()) {
							break
						}
						ch = commentWhitespace()
					}
					if ch != '(' {
						break
					}
					pos++
					ch = commentWhitespace()
					if ch != ')' {
						break
					}
					pos++
					ch = commentWhitespace()
					if ch != 'r' {
						break
					}
					if !strings.HasPrefix(source[pos+1:], "eturn") {
						break
					}
					pos += 6
					ch = commentWhitespace()
					if ch == '.' {
						pos++
						commentWhitespace()
						if !identifier() {
							break
						}
						ch = commentWhitespace()
					} else if ch == '[' {
						pos++
						ch = commentWhitespace()
						if ch == '\'' || ch == '"' {
							stringLiteral(ch)
						} else {
							break
						}
						pos++
						ch = commentWhitespace()
						if ch != ']' {
							break
						}
						pos++
						ch = commentWhitespace()
					}
					if ch == ';' {
						pos++
						ch = commentWhitespace()
					}
					if ch != '}' {
						break
					}
					pos++
					ch = commentWhitespace()
					if ch == ',' {
						pos++
						ch = commentWhitespace()
					}
					if ch != '}' {
						break
					}
					pos++
					ch = commentWhitespace()
					if ch != ')' {
						break
					}
					exports[decode(expt)] = struct{}{}
					return
				}
				break
			}
			if expt != "" {
				unsafeGetters[decode(expt)] = struct{}{}
			}
		} else if keys && ch == 'k' && strings.HasPrefix(source[pos+1:], "eys") {
			for true {
				pos += 4
				revertPos = pos - 1
				ch = commentWhitespace()
				if ch != '(' {
					break
				}
				pos++
				ch = commentWhitespace()
				idStart := pos
				if !identifier() {
					break
				}
				id := source[idStart:pos]
				ch = commentWhitespace()
				if ch != ')' {
					break
				}

				revertPos = pos
				pos++
				ch = commentWhitespace()
				if ch != '.' {
					break
				}
				pos++
				ch = commentWhitespace()
				if ch != 'f' || !strings.HasPrefix(source[pos+1:], "orEach") {
					break
				}
				pos += 7
				ch = commentWhitespace()
				revertPos = pos - 1
				//TODO FINISH
			}
		}
	}
}

func isParenKeyword(curPos int) bool {
	return source[curPos] == 'e' && strings.HasPrefix(source[curPos-4:], "whil") || source[curPos] == 'r' && strings.HasPrefix(source[curPos-2:], "fo") || source[curPos-1] == 'i' && source[curPos] == 'f'
}

func isPunctuator(ch byte) bool {
	return ch == '!' || ch == '%' || ch == '&' || ch > 39 && ch < 48 || ch > 57 && ch < 64 || ch == '[' || ch == ']' || ch == '^' || ch > 122 && ch < 127
}

func isExpressionPunctuator(ch byte) bool {
	return ch == '!' || ch == '%' || ch == '&' || ch > 39 && ch < 47 && ch != 41 || ch > 57 && ch < 64 || ch == '[' || ch == '^' || ch > 122 && ch < 127 && ch != '}'
}

func isExpressionTerminator(curPos int) bool {
	switch source[curPos] {
	case '>':
		return source[curPos-1] == '='
	case ';':
		fallthrough
	case ')':
		return true
	case 'h':
		return strings.HasPrefix(source[curPos-4:], "catc")
	case 'y':
		return strings.HasPrefix(source[curPos-6:], "finall")
	case 'e':
		return strings.HasPrefix(source[curPos-4:], "els")
	}
	return false
}

func Parse(source string, name sql.NullString) (Exports, error) {
	return parseCJS(source, name)
}
