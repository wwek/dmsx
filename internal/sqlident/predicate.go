package sqlident

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A filter is parsed, not filtered. The parser knows the shapes an operator
// needs to narrow an archive: a column compared with a constant, combined
// with AND, OR and NOT. A function call, a subquery, a SELECT, a table
// function or a placeholder has no rule, so it cannot be written. The
// predicate is rendered back from the parsed tree, so no text the operator
// typed reaches the statement.

// Predicate is a parsed filter expression.
type Predicate struct {
	root node
}

// ParsePredicate reads a filter expression and returns it.
func ParsePredicate(text string) (Predicate, error) {
	if strings.TrimSpace(text) == "" {
		return Predicate{}, fmt.Errorf("filter is empty")
	}
	if len(text) > 4000 {
		return Predicate{}, fmt.Errorf("filter is longer than 4000 characters")
	}
	p := &parser{text: text}
	root, err := p.parseOr()
	if err != nil {
		return Predicate{}, err
	}
	p.skipSpace()
	if p.pos != len(p.text) {
		return Predicate{}, p.errorf("%q is not allowed after the expression", p.text[p.pos:])
	}
	return Predicate{root: root}, nil
}

// ValidatePredicate reports whether the filter is one of the accepted shapes.
func ValidatePredicate(text string) error {
	_, err := ParsePredicate(text)
	return err
}

// MySQL renders the predicate for a MySQL or MariaDB statement.
func (p Predicate) MySQL() string {
	return p.render(dialectMySQL)
}

// ANSI renders the predicate for a statement that quotes identifiers with
// double quotes, the way DuckDB does.
func (p Predicate) ANSI() string {
	return p.render(dialectANSI)
}

// Empty reports whether the predicate holds no expression.
func (p Predicate) Empty() bool {
	return p.root == nil
}

func (p Predicate) render(d dialect) string {
	if p.root == nil {
		return ""
	}
	return p.root.render(d)
}

type dialect int

const (
	dialectMySQL dialect = iota
	dialectANSI
)

func (d dialect) quote(name string) string {
	if d == dialectMySQL {
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// node is one part of a parsed filter.
type node interface {
	render(dialect) string
}

type logicNode struct {
	op          string
	left, right node
}

func (n *logicNode) render(d dialect) string {
	return "(" + n.left.render(d) + " " + n.op + " " + n.right.render(d) + ")"
}

type notNode struct {
	inner node
}

func (n *notNode) render(d dialect) string {
	return "(NOT (" + n.inner.render(d) + "))"
}

type compareNode struct {
	column string
	op     string
	value  literal
}

func (n *compareNode) render(d dialect) string {
	return d.quote(n.column) + " " + n.op + " " + n.value.text
}

type nullNode struct {
	column string
	not    bool
}

func (n *nullNode) render(d dialect) string {
	if n.not {
		return d.quote(n.column) + " IS NOT NULL"
	}
	return d.quote(n.column) + " IS NULL"
}

type inNode struct {
	column string
	values []literal
}

func (n *inNode) render(d dialect) string {
	parts := make([]string, 0, len(n.values))
	for _, value := range n.values {
		parts = append(parts, value.text)
	}
	return d.quote(n.column) + " IN (" + strings.Join(parts, ", ") + ")"
}

type likeNode struct {
	column string
	value  literal
}

func (n *likeNode) render(d dialect) string {
	return d.quote(n.column) + " LIKE " + n.value.text
}

// literal is a constant the operator typed.
type literal struct {
	text   string
	isText bool
}

// reservedWords are the words a filter must not use as a column name. A
// quoted column named after one of them is harmless, but a filter that names
// one of them is trying to be a statement.
var reservedWords = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "TABLE": true, "AND": true,
	"OR": true, "NOT": true, "IN": true, "IS": true, "NULL": true, "LIKE": true,
	"UNION": true, "INTERSECT": true, "EXCEPT": true, "JOIN": true, "ON": true,
	"INSERT": true, "UPDATE": true, "DELETE": true, "DROP": true, "CREATE": true,
	"ALTER": true, "TRUNCATE": true, "GRANT": true, "REVOKE": true, "USE": true,
	"SHOW": true, "DESCRIBE": true, "EXPLAIN": true, "SET": true, "INTO": true,
	"VALUES": true, "ATTACH": true, "DETACH": true, "INSTALL": true, "LOAD": true,
	"COPY": true, "PRAGMA": true, "CALL": true, "EXEC": true, "EXECUTE": true,
	"CASE": true, "WHEN": true, "THEN": true, "ELSE": true, "END": true,
	"BETWEEN": true, "EXISTS": true, "CAST": true, "DISTINCT": true,
	"CURRENT_DATE": true, "CURRENT_TIME": true, "CURRENT_TIMESTAMP": true,
	"CURRENT_USER": true, "CURRENT_SCHEMA": true, "SESSION_USER": true,
	"TRUE": true, "FALSE": true, "ILIKE": true, "REGEXP": true, "RLIKE": true,
}

type parser struct {
	text string
	pos  int
}

func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("filter "+format, args...)
}

func (p *parser) skipSpace() {
	for p.pos < len(p.text) {
		switch p.text[p.pos] {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) peek() byte {
	if p.pos >= len(p.text) {
		return 0
	}
	return p.text[p.pos]
}

// readWord reads a run of letters, digits and underscores.
func (p *parser) readWord() string {
	start := p.pos
	for p.pos < len(p.text) {
		r, size := utf8.DecodeRuneInString(p.text[p.pos:])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		p.pos += size
	}
	return p.text[start:p.pos]
}

// hasKeyword reports whether the text at the current position is the word.
func (p *parser) hasKeyword(word string) bool {
	if p.pos+len(word) > len(p.text) {
		return false
	}
	if !strings.EqualFold(p.text[p.pos:p.pos+len(word)], word) {
		return false
	}
	end := p.pos + len(word)
	if end < len(p.text) {
		r, _ := utf8.DecodeRuneInString(p.text[end:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			return false
		}
	}
	return true
}

func (p *parser) parseOr() (node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpace()
		if !p.hasKeyword("OR") {
			return left, nil
		}
		p.pos += 2
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &logicNode{op: "OR", left: left, right: right}
	}
}

func (p *parser) parseAnd() (node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpace()
		if !p.hasKeyword("AND") {
			return left, nil
		}
		p.pos += 3
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &logicNode{op: "AND", left: left, right: right}
	}
}

func (p *parser) parseUnary() (node, error) {
	p.skipSpace()
	if p.hasKeyword("NOT") {
		p.pos += 3
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &notNode{inner: inner}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (node, error) {
	p.skipSpace()
	if p.peek() == '(' {
		p.pos++
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.peek() != ')' {
			return nil, p.errorf("the parenthesis is not closed")
		}
		p.pos++
		return inner, nil
	}
	return p.parseComparison()
}

func (p *parser) parseComparison() (node, error) {
	column, err := p.parseColumn()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	switch {
	case p.hasKeyword("IS"):
		p.pos += 2
		p.skipSpace()
		not := false
		if p.hasKeyword("NOT") {
			not = true
			p.pos += 3
			p.skipSpace()
		}
		if !p.hasKeyword("NULL") {
			return nil, p.errorf("IS needs NULL after it")
		}
		p.pos += 4
		return &nullNode{column: column, not: not}, nil
	case p.hasKeyword("LIKE"):
		p.pos += 4
		value, ok, err := p.parseLiteral()
		if err != nil {
			return nil, err
		}
		if !ok || !value.isText {
			return nil, p.errorf("LIKE needs a string constant")
		}
		return &likeNode{column: column, value: value}, nil
	case p.hasKeyword("IN"):
		p.pos += 2
		p.skipSpace()
		if p.peek() != '(' {
			return nil, p.errorf("IN needs a list of constants")
		}
		p.pos++
		var values []literal
		for {
			value, ok, err := p.parseLiteral()
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, p.errorf("IN needs a list of constants")
			}
			values = append(values, value)
			p.skipSpace()
			if p.peek() == ',' {
				p.pos++
				continue
			}
			break
		}
		p.skipSpace()
		if p.peek() != ')' {
			return nil, p.errorf("the IN list is not closed")
		}
		p.pos++
		if len(values) == 0 {
			return nil, p.errorf("IN needs at least one constant")
		}
		return &inNode{column: column, values: values}, nil
	}
	op := p.readOperator()
	if op == "" {
		return nil, p.errorf("expected a comparison after %q", column)
	}
	value, ok, err := p.parseLiteral()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, p.errorf("a comparison needs a constant on the right")
	}
	return &compareNode{column: column, op: op, value: value}, nil
}

// parseColumn reads the column a comparison is about.
func (p *parser) parseColumn() (string, error) {
	p.skipSpace()
	word := p.readWord()
	if word == "" {
		return "", p.errorf("expected a column name")
	}
	if !Valid(word) {
		return "", p.errorf("%q is not a plain column name", word)
	}
	if reservedWords[strings.ToUpper(word)] {
		return "", p.errorf("%q is not allowed in a filter", word)
	}
	p.skipSpace()
	if p.peek() == '(' {
		return "", p.errorf("function calls are not allowed in a filter")
	}
	return word, nil
}

// readOperator reads a comparison operator.
func (p *parser) readOperator() string {
	for _, op := range []string{"!=", "<>", "<=", ">=", "=", "<", ">"} {
		if strings.HasPrefix(p.text[p.pos:], op) {
			p.pos += len(op)
			return op
		}
	}
	return ""
}

// parseLiteral reads a constant. It reports false when the text at the
// current position is not a constant.
func (p *parser) parseLiteral() (literal, bool, error) {
	p.skipSpace()
	if p.pos >= len(p.text) {
		return literal{}, false, nil
	}
	switch ch := p.text[p.pos]; {
	case ch == '\'':
		return p.parseString()
	case ch == '-' || ch == '+':
		sign := p.pos
		p.pos++
		_, ok, err := p.parseNumber()
		if err != nil || !ok {
			p.pos = sign
			return literal{}, false, nil
		}
		return literal{text: p.text[sign:p.pos]}, true, nil
	case ch >= '0' && ch <= '9', ch == '.':
		return p.parseNumber()
	}
	word := p.readWord()
	switch strings.ToUpper(word) {
	case "TRUE":
		return literal{text: "TRUE"}, true, nil
	case "FALSE":
		return literal{text: "FALSE"}, true, nil
	case "NULL":
		return literal{text: "NULL"}, true, nil
	}
	return literal{}, false, nil
}

// parseString reads a single quoted constant.
func (p *parser) parseString() (literal, bool, error) {
	p.pos++
	var value strings.Builder
	for p.pos < len(p.text) {
		ch := p.text[p.pos]
		if ch == '\'' {
			if p.pos+1 < len(p.text) && p.text[p.pos+1] == '\'' {
				value.WriteString("''")
				p.pos += 2
				continue
			}
			p.pos++
			return literal{text: "'" + value.String() + "'", isText: true}, true, nil
		}
		value.WriteByte(ch)
		p.pos++
	}
	return literal{}, false, p.errorf("the string constant is not closed")
}

// parseNumber reads a numeric constant.
func (p *parser) parseNumber() (literal, bool, error) {
	start := p.pos
	for p.pos < len(p.text) && p.text[p.pos] >= '0' && p.text[p.pos] <= '9' {
		p.pos++
	}
	if p.peek() == '.' {
		p.pos++
		for p.pos < len(p.text) && p.text[p.pos] >= '0' && p.text[p.pos] <= '9' {
			p.pos++
		}
	}
	if p.peek() == 'e' || p.peek() == 'E' {
		mark := p.pos
		p.pos++
		if p.peek() == '+' || p.peek() == '-' {
			p.pos++
		}
		digits := p.pos
		for p.pos < len(p.text) && p.text[p.pos] >= '0' && p.text[p.pos] <= '9' {
			p.pos++
		}
		if p.pos == digits {
			p.pos = mark
		}
	}
	text := p.text[start:p.pos]
	if text == "" || text == "." {
		p.pos = start
		return literal{}, false, nil
	}
	if _, err := strconv.ParseFloat(text, 64); err != nil {
		p.pos = start
		return literal{}, false, p.errorf("%q is not a number", text)
	}
	return literal{text: text}, true, nil
}
