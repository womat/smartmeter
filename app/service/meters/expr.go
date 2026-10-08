package meters

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

type exprParser struct {
	input string
	pos   int
	vars  map[string]float64
}

func evalExpression(expr string, vars map[string]float64) (float64, error) {
	replacer := strings.NewReplacer("${", "", "{", "", "}", "")
	p := &exprParser{
		input: replacer.Replace(expr),
		vars:  vars,
	}
	value, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if p.pos != len(p.input) {
		return 0, fmt.Errorf("unexpected token at position %d", p.pos)
	}
	return value, nil
}

func (p *exprParser) parseExpr() (float64, error) {
	left, err := p.parseTerm()
	if err != nil {
		return 0, err
	}

	for {
		p.skipSpace()
		switch p.peek() {
		case '+':
			p.pos++
			right, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			left += right
		case '-':
			p.pos++
			right, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			left -= right
		default:
			return left, nil
		}
	}
}

func (p *exprParser) parseTerm() (float64, error) {
	left, err := p.parseFactor()
	if err != nil {
		return 0, err
	}

	for {
		p.skipSpace()
		switch p.peek() {
		case '*':
			p.pos++
			right, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			left *= right
		case '/':
			p.pos++
			right, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			if right == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			left /= right
		default:
			return left, nil
		}
	}
}

func (p *exprParser) parseFactor() (float64, error) {
	p.skipSpace()

	switch ch := p.peek(); ch {
	case '+':
		p.pos++
		return p.parseFactor()
	case '-':
		p.pos++
		value, err := p.parseFactor()
		return -value, err
	case '(':
		p.pos++
		value, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		p.skipSpace()
		if p.peek() != ')' {
			return 0, fmt.Errorf("missing closing parenthesis")
		}
		p.pos++
		return value, nil
	}

	if isIdentifierStart(p.peek()) {
		ident := p.parseIdentifier()
		p.skipSpace()
		if p.peek() == '(' {
			p.pos++
			arg, err := p.parseExpr()
			if err != nil {
				return 0, err
			}
			p.skipSpace()
			if p.peek() != ')' {
				return 0, fmt.Errorf("missing closing parenthesis for %s", ident)
			}
			p.pos++

			switch strings.ToUpper(ident) {
			case "SQRT":
				if arg < 0 {
					return 0, fmt.Errorf("sqrt of negative value")
				}
				return math.Sqrt(arg), nil
			case "ABS":
				return math.Abs(arg), nil
			default:
				return 0, fmt.Errorf("unsupported function %s", ident)
			}
		}

		value, ok := p.vars[ident]
		if !ok {
			return 0, fmt.Errorf("unknown field %s", ident)
		}
		return value, nil
	}

	return p.parseNumber()
}

func (p *exprParser) parseNumber() (float64, error) {
	start := p.pos
	dotSeen := false

	for p.pos < len(p.input) {
		ch := rune(p.input[p.pos])
		if ch == '.' {
			if dotSeen {
				break
			}
			dotSeen = true
			p.pos++
			continue
		}
		if !unicode.IsDigit(ch) {
			break
		}
		p.pos++
	}

	if start == p.pos {
		return 0, fmt.Errorf("expected number at position %d", p.pos)
	}
	return strconv.ParseFloat(strings.TrimSpace(p.input[start:p.pos]), 64)
}

func (p *exprParser) parseIdentifier() string {
	start := p.pos
	for p.pos < len(p.input) {
		ch := rune(p.input[p.pos])
		if !isIdentifierPart(ch) {
			break
		}
		p.pos++
	}
	return strings.TrimSpace(p.input[start:p.pos])
}

func (p *exprParser) skipSpace() {
	for p.pos < len(p.input) && unicode.IsSpace(rune(p.input[p.pos])) {
		p.pos++
	}
}

func (p *exprParser) peek() rune {
	if p.pos >= len(p.input) {
		return 0
	}
	return rune(p.input[p.pos])
}

func isIdentifierStart(ch rune) bool {
	return unicode.IsLetter(ch) || ch == '_'
}

func isIdentifierPart(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_'
}
