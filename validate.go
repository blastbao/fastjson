package fastjson

import (
	"fmt"
	"strconv"
	"strings"
)

// Validate validates JSON s.
func Validate(s string) error {
	s = skipWS(s)

	tail, err := validateValue(s)
	if err != nil {
		return fmt.Errorf("cannot parse JSON: %s; unparsed tail: %q", err, startEndString(tail))
	}
	tail = skipWS(tail)
	if len(tail) > 0 {
		return fmt.Errorf("unexpected tail: %q", startEndString(tail))
	}
	return nil
}

// ValidateBytes validates JSON b.
func ValidateBytes(b []byte) error {
	return Validate(b2s(b))
}

func validateValue(s string) (string, error) {
	if len(s) == 0 {
		return s, fmt.Errorf("cannot parse empty string")
	}

	if s[0] == '{' {
		tail, err := validateObject(s[1:])
		if err != nil {
			return tail, fmt.Errorf("cannot parse object: %s", err)
		}
		return tail, nil
	}
	if s[0] == '[' {
		tail, err := validateArray(s[1:])
		if err != nil {
			return tail, fmt.Errorf("cannot parse array: %s", err)
		}
		return tail, nil
	}
	if s[0] == '"' {
		sv, tail, err := validateString(s[1:])
		if err != nil {
			return tail, fmt.Errorf("cannot parse string: %s", err)
		}
		// Scan the string for control chars.
		for i := 0; i < len(sv); i++ {
			if sv[i] < 0x20 {
				return tail, fmt.Errorf("string cannot contain control char 0x%02X", sv[i])
			}
		}
		return tail, nil
	}
	if s[0] == 't' {
		if len(s) < len("true") || s[:len("true")] != "true" {
			return s, fmt.Errorf("unexpected value found: %q", s)
		}
		return s[len("true"):], nil
	}
	if s[0] == 'f' {
		if len(s) < len("false") || s[:len("false")] != "false" {
			return s, fmt.Errorf("unexpected value found: %q", s)
		}
		return s[len("false"):], nil
	}
	if s[0] == 'n' {
		if len(s) < len("null") || s[:len("null")] != "null" {
			return s, fmt.Errorf("unexpected value found: %q", s)
		}
		return s[len("null"):], nil
	}

	tail, err := validateNumber(s)
	if err != nil {
		return tail, fmt.Errorf("cannot parse number: %s", err)
	}
	return tail, nil
}

func validateArray(s string) (string, error) {
	s = skipWS(s)
	if len(s) == 0 {
		return s, fmt.Errorf("missing ']'")
	}
	if s[0] == ']' {
		return s[1:], nil
	}

	for {
		var err error

		s = skipWS(s)
		s, err = validateValue(s)
		if err != nil {
			return s, fmt.Errorf("cannot parse array value: %s", err)
		}

		s = skipWS(s)
		if len(s) == 0 {
			return s, fmt.Errorf("unexpected end of array")
		}
		if s[0] == ',' {
			s = s[1:]
			continue
		}
		if s[0] == ']' {
			s = s[1:]
			return s, nil
		}
		return s, fmt.Errorf("missing ',' after array value")
	}
}

func validateObject(s string) (string, error) {
	s = skipWS(s)
	if len(s) == 0 {
		return s, fmt.Errorf("missing '}'")
	}
	if s[0] == '}' {
		return s[1:], nil
	}

	for {
		var err error

		// Parse key.
		s = skipWS(s)
		if len(s) == 0 || s[0] != '"' {
			return s, fmt.Errorf(`cannot find opening '"" for object key`)
		}

		var key string
		key, s, err = validateKey(s[1:])
		if err != nil {
			return s, fmt.Errorf("cannot parse object key: %s", err)
		}
		// Scan the key for control chars.
		for i := 0; i < len(key); i++ {
			if key[i] < 0x20 {
				return s, fmt.Errorf("object key cannot contain control char 0x%02X", key[i])
			}
		}
		s = skipWS(s)
		if len(s) == 0 || s[0] != ':' {
			return s, fmt.Errorf("missing ':' after object key")
		}
		s = s[1:]

		// Parse value
		s = skipWS(s)
		s, err = validateValue(s)
		if err != nil {
			return s, fmt.Errorf("cannot parse object value: %s", err)
		}
		s = skipWS(s)
		if len(s) == 0 {
			return s, fmt.Errorf("unexpected end of object")
		}
		if s[0] == ',' {
			s = s[1:]
			continue
		}
		if s[0] == '}' {
			return s[1:], nil
		}
		return s, fmt.Errorf("missing ',' after object value")
	}
}

// validateKey is similar to validateString, but is optimized
// for typical object keys, which are quite small and have no escape sequences.
//
// 快速路径：遍历字符串，
//   - 如果遇到 "，说明 key 完整结束，直接返回。
//   - 如果遇到 \，说明 key 包含转义字符，需要走慢路径。
//
// 慢路径：解析出字符串，若其中包含转义符则检查是否合法
func validateKey(s string) (string, string, error) {
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			// Fast path - the key doesn't contain escape sequences.
			return s[:i], s[i+1:], nil
		}
		if s[i] == '\\' {
			// Slow path - the key contains escape sequences.
			return validateString(s)
		}
	}
	return "", s, fmt.Errorf(`missing closing '"'`)
}

// 先从 s 中解析和提取出 "..." 的字符串，然后检查其中的转义符号是否合法。
func validateString(s string) (string, string, error) {
	// Try fast path - a string without escape sequences.
	//
	// 快速路径：如果字符串里没有 \ ，直接把开头到下一个 " 的内容作为解析结果，返回剩余字符串（跳过这个引号）。
	if n := strings.IndexByte(s, '"'); n >= 0 && strings.IndexByte(s[:n], '\\') < 0 {
		return s[:n], s[n+1:], nil
	}

	// Slow path - escape sequences are present.

	// 先调用 parseRawString 提取原始字符串（包含转义序列）
	rs, tail, err := parseRawString(s)
	if err != nil {
		return rs, tail, err
	}

	// 循环寻找 \ 来检查转义序列是否合法
	for {
		n := strings.IndexByte(rs, '\\')
		if n < 0 { // 没有更多转义序列，返回成功
			return rs, tail, nil
		}
		n++
		if n >= len(rs) {
			return rs, tail, fmt.Errorf("BUG: parseRawString returned invalid string with trailing backslash: %q", rs)
		}
		ch := rs[n]
		rs = rs[n+1:]
		switch ch {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			// Valid escape sequences - see http://json.org/
			// 有效转义序列
			break
		case 'u':
			// Unicode 转义序列
			if len(rs) < 4 {
				return rs, tail, fmt.Errorf(`too short escape sequence: \u%s`, rs)
			}
			xs := rs[:4]                            // 提取4位十六进制数字
			_, err := strconv.ParseUint(xs, 16, 16) // 验证十六进制
			if err != nil {
				return rs, tail, fmt.Errorf(`invalid escape sequence \u%s: %s`, xs, err)
			}
			rs = rs[4:] // 跳过已处理的Unicode序列
		default:
			return rs, tail, fmt.Errorf(`unknown escape sequence \%c`, ch)
		}
	}
}

func validateNumber(s string) (string, error) {
	if len(s) == 0 {
		return s, fmt.Errorf("zero-length number")
	}

	// 如果数字以 - 开头，其后必须至少有一位数字，否则报错。
	if s[0] == '-' {
		s = s[1:]
		if len(s) == 0 {
			return s, fmt.Errorf("missing number after minus")
		}
	}

	// 解析连续的数字。
	// - 如果 i <= 0 表示开头没有数字，报错。
	// - 如果以 0 开头且长度 >1 报错，因为 JSON 不允许 0123 这种数字。
	i := 0
	for i < len(s) {
		if s[i] < '0' || s[i] > '9' {
			break
		}
		i++
	}
	if i <= 0 {
		return s, fmt.Errorf("expecting 0..9 digit, got %c", s[0])
	}
	if s[0] == '0' && i != 1 {
		return s, fmt.Errorf("unexpected number starting from 0")
	}
	// 到达末尾，意味着全是数字，没有剩余字符串，直接返回。
	if i >= len(s) {
		return "", nil
	}

	// 遇到小数点，其后必须至少有一位数字，否则报错。
	if s[i] == '.' {
		// Validate fractional part
		s = s[i+1:]
		if len(s) == 0 {
			return s, fmt.Errorf("missing fractional part")
		}
		i = 0
		for i < len(s) {
			if s[i] < '0' || s[i] > '9' {
				break
			}
			i++
		}
		if i == 0 {
			return s, fmt.Errorf("expecting 0..9 digit in fractional part, got %c", s[0])
		}
		// 到达末尾，意味着全是数字，没有剩余字符串，直接返回。
		if i >= len(s) {
			return "", nil
		}
	}

	// 遇到 e 或 E，说明是科学计数法。
	//	- 指数部分可以有 + 或 -。
	//	- 指数必须至少有一位数字，否则报错。
	if s[i] == 'e' || s[i] == 'E' {
		// Validate exponent part
		s = s[i+1:]
		if len(s) == 0 {
			return s, fmt.Errorf("missing exponent part")
		}
		if s[0] == '-' || s[0] == '+' {
			s = s[1:]
			if len(s) == 0 {
				return s, fmt.Errorf("missing exponent part")
			}
		}
		i = 0
		for i < len(s) {
			if s[i] < '0' || s[i] > '9' {
				break
			}
			i++
		}
		if i == 0 {
			return s, fmt.Errorf("expecting 0..9 digit in exponent part, got %c", s[0])
		}
		if i >= len(s) {
			return "", nil
		}
	}
	return s[i:], nil
}
