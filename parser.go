package fastjson

import (
	"fmt"
	"github.com/valyala/fastjson/fastfloat"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Parser parses JSON.
//
// Parser may be re-used for subsequent parsing.
//
// Parser cannot be used from concurrent goroutines.
// Use per-goroutine parsers or ParserPool instead.
type Parser struct {
	// b contains working copy of the string to be parsed.
	b []byte

	// c is a cache for json values.
	c cache
}

// Parse parses s containing JSON.
//
// The returned value is valid until the next call to Parse*.
//
// Use Scanner if a stream of JSON values must be parsed.
func (p *Parser) Parse(s string) (*Value, error) {
	s = skipWS(s)
	p.b = append(p.b[:0], s...)
	p.c.reset()

	v, tail, err := parseValue(b2s(p.b), &p.c, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot parse JSON: %s; unparsed tail: %q", err, startEndString(tail))
	}
	tail = skipWS(tail)
	if len(tail) > 0 {
		return nil, fmt.Errorf("unexpected tail: %q", startEndString(tail))
	}
	return v, nil
}

// ParseBytes parses b containing JSON.
//
// The returned Value is valid until the next call to Parse*.
//
// Use Scanner if a stream of JSON values must be parsed.
func (p *Parser) ParseBytes(b []byte) (*Value, error) {
	return p.Parse(b2s(b))
}

type cache struct {
	vs []Value
}

func (c *cache) reset() {
	c.vs = c.vs[:0]
}

// 从缓存中获取一个可用的 Value 对象
func (c *cache) getValue() *Value {
	// 切片未满，通过调整切片长度来"激活"一个预分配的元素；这里没有分配新内存，只是扩展切片的可见部分。
	if cap(c.vs) > len(c.vs) {
		c.vs = c.vs[:len(c.vs)+1]
	} else {
		// 切片已满，用 append 添加一个新的 Value{} ；Go 会自动处理底层数组的扩容。
		c.vs = append(c.vs, Value{})
	}
	// Do not reset the value, since the caller must properly init it.
	// 返回切片中最后一个元素的地址，这个元素要么是新激活的预分配元素，要么是新追加的元素。
	return &c.vs[len(c.vs)-1]
}

// 跳过字符串的前导空白字符
func skipWS(s string) string {
	if len(s) == 0 || s[0] > 0x20 { // ASCII 码大于 0x20(32) 的字符都是非空白字符，小于等于 32 的字符可能是空白字符或其他控制字符
		// Fast path.
		return s
	}
	return skipWSSlow(s)
}

func skipWSSlow(s string) string {
	// 检查第一个字符是否是四种空白字符之一
	if len(s) == 0 || s[0] != 0x20 && s[0] != 0x0A && s[0] != 0x09 && s[0] != 0x0D {
		return s
	}
	// 循环跳过前导空白字符
	for i := 1; i < len(s); i++ {
		if s[i] != 0x20 && s[i] != 0x0A && s[i] != 0x09 && s[i] != 0x0D {
			return s[i:]
		}
	}
	return ""
}

type kv struct {
	k string
	v *Value
}

// MaxDepth is the maximum depth for nested JSON.
const MaxDepth = 300

func parseValue(s string, c *cache, depth int) (*Value, string, error) {
	if len(s) == 0 {
		return nil, s, fmt.Errorf("cannot parse empty string")
	}

	// 深度控制，防止栈溢出
	depth++
	if depth > MaxDepth {
		return nil, s, fmt.Errorf("too big depth for the nested JSON; it exceeds %d", MaxDepth)
	}

	// 根据 s[0] 的首字符，判断当前值的类型：
	//	'{' → 调 parseObject
	//	'[' → 调 parseArray
	//	'"' → 调 parseRawString
	//	't' → 必须是 true
	//	'f' → 必须是 false
	//	'n' → 必须是 null 或 nan
	//	其他 → 当作 number 调 parseRawNumber
	if s[0] == '{' {
		v, tail, err := parseObject(s[1:], c, depth)
		if err != nil {
			return nil, tail, fmt.Errorf("cannot parse object: %s", err)
		}
		return v, tail, nil
	}
	if s[0] == '[' {
		v, tail, err := parseArray(s[1:], c, depth)
		if err != nil {
			return nil, tail, fmt.Errorf("cannot parse array: %s", err)
		}
		return v, tail, nil
	}
	if s[0] == '"' {
		ss, tail, err := parseRawString(s[1:])
		if err != nil {
			return nil, tail, fmt.Errorf("cannot parse string: %s", err)
		}
		v := c.getValue()
		v.t = typeRawString
		v.s = ss
		return v, tail, nil
	}
	if s[0] == 't' {
		if len(s) < len("true") || s[:len("true")] != "true" {
			return nil, s, fmt.Errorf("unexpected value found: %q", s)
		}
		return valueTrue, s[len("true"):], nil
	}
	if s[0] == 'f' {
		if len(s) < len("false") || s[:len("false")] != "false" {
			return nil, s, fmt.Errorf("unexpected value found: %q", s)
		}
		return valueFalse, s[len("false"):], nil
	}
	if s[0] == 'n' {
		if len(s) < len("null") || s[:len("null")] != "null" {
			// Try parsing NaN
			if len(s) >= 3 && strings.EqualFold(s[:3], "nan") {
				v := c.getValue()
				v.t = TypeNumber
				v.s = s[:3]
				return v, s[3:], nil
			}
			return nil, s, fmt.Errorf("unexpected value found: %q", s)
		}
		return valueNull, s[len("null"):], nil
	}

	ns, tail, err := parseRawNumber(s)
	if err != nil {
		return nil, tail, fmt.Errorf("cannot parse number: %s", err)
	}
	v := c.getValue()
	v.t = TypeNumber
	v.s = ns
	return v, tail, nil
}

func parseArray(s string, c *cache, depth int) (*Value, string, error) {
	// 先跳过前导空白
	s = skipWS(s)
	// 如果 s 为空，说明缺少 ]，直接报错
	if len(s) == 0 {
		return nil, s, fmt.Errorf("missing ']'")
	}
	// 如果遇到 ] ，说明是一个 空数组，直接返回一个 TypeArray 类型的 Value，其中 a 被清空（v.a[:0]），然后跳过 ] 。
	if s[0] == ']' {
		v := c.getValue()
		v.t = TypeArray
		v.a = v.a[:0]
		return v, s[1:], nil
	}

	// 创建数组节点
	a := c.getValue()
	a.t = TypeArray
	a.a = a.a[:0]

	// 循环解析数组元素
	for {
		var v *Value
		var err error

		/// 调用 parseValue 解析下一个值，将解析出的值追加到数组中。
		s = skipWS(s)
		v, s, err = parseValue(s, c, depth)
		if err != nil {
			return nil, s, fmt.Errorf("cannot parse array value: %s", err)
		}
		a.a = append(a.a, v)

		/// 处理分隔符 , 或结束符 ]
		s = skipWS(s)
		if len(s) == 0 { // 如果输入用完了，还没遇到 ]，报错。
			return nil, s, fmt.Errorf("unexpected end of array")
		}
		if s[0] == ',' { // 如果遇到 ,，说明后面还有元素，跳过 , 进入下一轮循环。
			s = s[1:]
			continue
		}
		if s[0] == ']' { // 如果遇到 ]，说明数组结束，返回结果。
			s = s[1:]
			return a, s, nil
		}

		/// 其他情况，说明格式不对，比如 [1 2] 少了逗号，报错。
		return nil, s, fmt.Errorf("missing ',' after array value")
	}
}

func parseObject(s string, c *cache, depth int) (*Value, string, error) {
	// 跳过前导空白
	s = skipWS(s)
	if len(s) == 0 { // 缺少闭合 } 字符。
		return nil, s, fmt.Errorf("missing '}'")
	}

	// 检查是否是空对象
	if s[0] == '}' {
		v := c.getValue()    // 从缓存中获取一个空 Value
		v.t = TypeObject     // 设置数据类型
		v.o.reset()          // 清空对象的键值对
		return v, s[1:], nil // 返回空对象，推进 s 来跳过 } 。
	}

	// 获取一个空对象
	o := c.getValue()
	o.t = TypeObject
	o.o.reset()

	// 循环解析键值对，直到遇到结束的 } 。
	for {
		var err error
		///// 在 Object 的内部键值对切片中分配一个新的槽位，返回指向该槽位的指针
		kv := o.o.getKV()

		///// 解析 key

		// 跳过前导空白
		s = skipWS(s)
		// 键必须是以双引号 " 开头的字符串
		if len(s) == 0 || s[0] != '"' {
			return nil, s, fmt.Errorf(`cannot find opening '"" for object key`)
		}
		// 跳过开头的 " ，解析出 key 并保存到 kv.k
		kv.k, s, err = parseRawKey(s[1:])
		if err != nil {
			return nil, s, fmt.Errorf("cannot parse object key: %s", err)
		}
		// 检查 : 分隔符
		s = skipWS(s)
		if len(s) == 0 || s[0] != ':' {
			return nil, s, fmt.Errorf("missing ':' after object key")
		}
		// 跳过 : 分隔符
		s = s[1:]

		///// 解析 value

		// 跳过前导空白
		s = skipWS(s)
		// 解析出 value 并保存到 kv.v
		kv.v, s, err = parseValue(s, c, depth)
		if err != nil {
			return nil, s, fmt.Errorf("cannot parse object value: %s", err)
		}
		s = skipWS(s)
		if len(s) == 0 {
			return nil, s, fmt.Errorf("unexpected end of object")
		}
		// 遇到 , 意味着还有更多键值对，跳过逗号继续循环
		if s[0] == ',' {
			s = s[1:]
			continue
		}
		// 遇到 } 意味着对象结束，跳过右花括号，返回解析结果
		if s[0] == '}' {
			return o, s[1:], nil
		}

		// 其它字符，报错
		return nil, s, fmt.Errorf("missing ',' after object value")
	}
}

func escapeString(dst []byte, s string) []byte {
	// 快速路径：
	//	当 s 不包含任何需要转义的特殊字符时，直接在 s 前后添加双引号后返回。
	if !hasSpecialChars(s) {
		// Fast path - nothing to escape.
		dst = append(dst, '"')
		dst = append(dst, s...)
		dst = append(dst, '"')
		return dst
	}

	// Slow path.
	// 当 s 包含需要转义的特殊字符时，使用 Go 标准库的 strconv.AppendQuote 函数来转义。
	return strconv.AppendQuote(dst, s)
}

// hasSpecialChars 判断字符串 s 中是否包含需要转义的特殊字符
//
// 需转义的字符类型：
//
//	∙ 双引号 (")：必须转义为 \" ，因为双引号用于标记字符串的边界。
//	∙ 反斜杠 (\)：必须转义为 \\ ，因为反斜杠本身是转义字符。
//	∙ 控制字符（ASCII < 0x20）：包括：
//		∙ \b (退格，0x08)
//		∙ \f (换页，0x0C)
//		∙ \n (换行，0x0A)
//		∙ \r (回车，0x0D)
//		∙ \t (制表符，0x09)
//		∙ 其他控制字符会转义为 \u00XX 形式
func hasSpecialChars(s string) bool {
	// 检查双引号和反斜杠
	if strings.IndexByte(s, '"') >= 0 || strings.IndexByte(s, '\\') >= 0 {
		return true
	}
	// 检查控制字符（ASCII < 0x20）
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 {
			return true
		}
	}
	return false
}

func unescapeStringBestEffort(s string) string {
	// 当字符串中不包含反斜杠 \ 时，直接返回原字符串，无需任何处理。
	n := strings.IndexByte(s, '\\')
	if n < 0 {
		return s // Fast path - nothing to unescape.
	}
	// Slow path - unescape string.

	// 当 s 中包含反斜杠时，进入详细的转义处理逻辑。
	b := s2b(s) // 直接把 string 转为 []byte ，这种转换是安全的，因为 s 指向 Parser.b 中的字节切片
	b = b[:n]   // 保留反斜杠前的部分，视作已解码内容
	s = s[n+1:] // 跳过反斜杠

	for len(s) > 0 {
		ch := s[0] // 取出反斜杠后的第一个字符
		s = s[1:]  // 把这个字符从剩余内容中去掉
		switch ch {
		case '"':
			b = append(b, '"') // 将 \" 转换为 "
		case '\\':
			b = append(b, '\\') // 将 \\ 转换为 \
		case '/':
			b = append(b, '/') // 将 \/ 转换为 /
		case 'b':
			b = append(b, '\b') // 将 \b 转换为退格符
		case 'f':
			b = append(b, '\f') // 将 \f 转换为换页符
		case 'n':
			b = append(b, '\n') // 将 \n 转换为换行符
		case 'r':
			b = append(b, '\r') // 将 \r 转换为回车符
		case 't':
			b = append(b, '\t') // 将 \t 转换为制表符，这里追加的 '\t' 看着和原始字符串一样，但实际含义不同
		case 'u':
			// \u 需要解析 4 或 8 个 hex 字符

			// 序列太短，保持原样
			if len(s) < 4 {
				// Too short escape sequence. Just store it unchanged.
				b = append(b, "\\u"...)
				break
			}

			// 提取 4 个十六进制字符，转换为 uint64
			xs := s[:4]
			x, err := strconv.ParseUint(xs, 16, 16)
			if err != nil {
				// Invalid escape sequence. Just store it unchanged.
				// 无效的十六进制，保持原样
				b = append(b, "\\u"...)
				break
			}

			// 消耗掉这4个字符
			s = s[4:]

			// 非代理对，直接转换
			if !utf16.IsSurrogate(rune(x)) {
				b = append(b, string(rune(x))...)
				break
			}

			// Surrogate.
			// See https://en.wikipedia.org/wiki/Universal_Character_Set_characters#Surrogates
			//
			// 处理代理对
			if len(s) < 6 || s[0] != '\\' || s[1] != 'u' {
				// 没有配对的代理项，保持原样
				b = append(b, "\\u"...)
				b = append(b, xs...)
				break
			}
			x1, err := strconv.ParseUint(s[2:6], 16, 16)
			if err != nil {
				b = append(b, "\\u"...)
				b = append(b, xs...)
				break
			}

			// 解码 UTF-16 代理对为完整的 Unicode 字符
			r := utf16.DecodeRune(rune(x), rune(x1))
			b = append(b, string(r)...)
			s = s[6:]
		default:
			// Unknown escape sequence. Just store it unchanged.
			// 未知转义序列，保持原样
			b = append(b, '\\', ch)
		}

		// 继续查找下一个反斜杠 \ 的位置，如果没有找到，直接追加剩余字符串并结束循环，否则继续处理下一个反斜杠。
		n = strings.IndexByte(s, '\\')
		if n < 0 {
			b = append(b, s...)
			break
		}
		b = append(b, s[:n]...)
		s = s[n+1:]
	}

	// 将处理后的字节切片转换回字符串返回
	return b2s(b)
}

// parseRawKey is similar to parseRawString, but is optimized
// for small-sized keys without escape sequences.
func parseRawKey(s string) (string, string, error) {
	// 扫描字符串，如果遇到一个 " 且其之前没有 \ ，说明这是 key 的结束引号。
	// 如果在扫描过程中先遇到了 \ 那么直接走 slow path 。
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			// Fast path.
			return s[:i], s[i+1:], nil
		}
		if s[i] == '\\' {
			// Slow path.
			return parseRawString(s)
		}
	}
	return s, "", fmt.Errorf(`missing closing '"'`)
}

func parseRawString(s string) (string, string, error) {
	// 找到第一个双引号的位置，这个引号是标识字符串结束的双引号
	n := strings.IndexByte(s, '"')
	// 没找到，报错
	if n < 0 {
		return s, "", fmt.Errorf(`missing closing '"'`)
	}
	// 如果双引号在开头 (n == 0)，或者双引号前不是反斜杠，说明这个双引号没有被转义，是真正的结束引号
	if n == 0 || s[n-1] != '\\' {
		// Fast path. No escaped ".
		// 返回第一个双引号前的内容和剩余字符串
		return s[:n], s[n+1:], nil
	}

	// Slow path - possible escaped " found.
	// 如果首个双引号被转义了，那么需要继续往后找，直到找到首个未转义的双引号，其标识着字符串结束。

	// 保存原始字符串的引用
	ss := s
	for {
		// 从 n-1（即首个 " 的前一个位置）往前数，统计连续的 \ 。
		i := n - 1
		for i > 0 && s[i-1] == '\\' {
			i--
		}

		// (n-i) = 连续 \ 的个数，如果是偶数，说明 " 没有被转义，此时 " 是合法的结束引号，如果是奇数，说明这个 " 是被转义的，继续查找。
		if uint(n-i)%2 == 0 {
			return ss[:len(ss)-len(s)+n], s[n+1:], nil
		}

		// 跳过这个被转义的 " ，继续查找下一个双引号。
		s = s[n+1:]
		n = strings.IndexByte(s, '"')
		if n < 0 {
			return ss, "", fmt.Errorf(`missing closing '"'`)
		}
		// 如果找到的 " 不是被转义的，直接返回，否则 continue
		if n == 0 || s[n-1] != '\\' {
			return ss[:len(ss)-len(s)+n], s[n+1:], nil
		}
	}
}

// 从字符串 s 的开头提取出一个数字字面量，并返回剩余字符串。
func parseRawNumber(s string) (string, string, error) {
	// The caller must ensure len(s) > 0

	// Find the end of the number.
	for i := 0; i < len(s); i++ {

		// 遍历字符串 s 的每个字符，如果是数、.、-/+、e/E，继续扫描，直到遇到非数字字符。
		ch := s[i]
		if (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == 'e' || ch == 'E' || ch == '+' {
			continue
		}

		// 两种情况：
		//	1）第一个字符就不是数字字符（比如 a, [, { 等），如 abc123
		//	2）第二个字符不是数字字符，但第一个字符是 - 或 + ，如 -a123
		// 此时，需要进一步检查是否是 Nan/+NaN/-NaN 或者是 Inf/+Inf/-Inf（大小写不敏感），
		// 如果不是 inf / nan ，则无法识别，直接报错，否则返回该原始字符串。
		if i == 0 || i == 1 && (s[0] == '-' || s[0] == '+') {
			if len(s[i:]) >= 3 {
				xs := s[i : i+3]
				if strings.EqualFold(xs, "inf") || strings.EqualFold(xs, "nan") {
					return s[:i+3], s[i+3:], nil
				}
			}
			return "", s, fmt.Errorf("unexpected char: %q", s[:1])
		}

		// 遇到第一个非数字字符，直接结束并返回
		ns := s[:i]
		s = s[i:]
		return ns, s, nil
	}
	return s, "", nil
}

// Object represents JSON object.
//
// Object cannot be used from concurrent goroutines.
// Use per-goroutine parsers or ParserPool instead.
type Object struct {
	kvs           []kv // 对象的键值对列表
	keysUnescaped bool // 优化标志，表示键是否是未转义的纯字符串
}

func (o *Object) reset() {
	o.kvs = o.kvs[:0]
	o.keysUnescaped = false
}

// MarshalTo appends marshaled o to dst and returns the result.
func (o *Object) MarshalTo(dst []byte) []byte {
	dst = append(dst, '{')
	for i, kv := range o.kvs {
		if o.keysUnescaped {
			dst = escapeString(dst, kv.k)
		} else {
			dst = append(dst, '"')
			dst = append(dst, kv.k...)
			dst = append(dst, '"')
		}
		dst = append(dst, ':')
		dst = kv.v.MarshalTo(dst)
		if i != len(o.kvs)-1 {
			dst = append(dst, ',')
		}
	}
	dst = append(dst, '}')
	return dst
}

// String returns string representation for the o.
//
// This function is for debugging purposes only. It isn't optimized for speed.
// See MarshalTo instead.
func (o *Object) String() string {
	b := o.MarshalTo(nil)
	// It is safe converting b to string without allocation, since b is no longer
	// reachable after this line.
	return b2s(b)
}

func (o *Object) getKV() *kv {
	if cap(o.kvs) > len(o.kvs) {
		o.kvs = o.kvs[:len(o.kvs)+1]
	} else {
		o.kvs = append(o.kvs, kv{})
	}
	return &o.kvs[len(o.kvs)-1]
}

func (o *Object) unescapeKeys() {
	if o.keysUnescaped {
		return
	}
	kvs := o.kvs
	for i := range kvs {
		kv := &kvs[i]
		kv.k = unescapeStringBestEffort(kv.k)
	}
	o.keysUnescaped = true
}

// Len returns the number of items in the o.
func (o *Object) Len() int {
	return len(o.kvs)
}

// Get returns the value for the given key in the o.
//
// Returns nil if the value for the given key isn't found.
//
// The returned value is valid until Parse is called on the Parser returned o.
func (o *Object) Get(key string) *Value {
	if !o.keysUnescaped && strings.IndexByte(key, '\\') < 0 {
		// Fast path - try searching for the key without object keys unescaping.
		for _, kv := range o.kvs {
			if kv.k == key {
				return kv.v
			}
		}
	}

	// Slow path - unescape object keys.
	o.unescapeKeys()

	for _, kv := range o.kvs {
		if kv.k == key {
			return kv.v
		}
	}
	return nil
}

// Visit calls f for each item in the o in the original order
// of the parsed JSON.
//
// f cannot hold key and/or v after returning.
func (o *Object) Visit(f func(key []byte, v *Value)) {
	if o == nil {
		return
	}

	o.unescapeKeys()

	for _, kv := range o.kvs {
		f(s2b(kv.k), kv.v)
	}
}

// Value represents any JSON value.
//
// Call Type in order to determine the actual type of the JSON value.
//
// Value cannot be used from concurrent goroutines.
// Use per-goroutine parsers or ParserPool instead.
type Value struct {
	o Object   // 对象类型
	a []*Value // 数组类型
	s string   // 字符串/数字类型
	t Type     // 类型标记
}

// MarshalTo appends marshaled v to dst and returns the result.
func (v *Value) MarshalTo(dst []byte) []byte {
	switch v.t {
	case typeRawString:
		// 原始字符串类型：
		//	∙ 在字符串前后添加双引号
		//	∙ 直接将字符串内容 v.s 追加到结果中，不做转义处理
		dst = append(dst, '"')
		dst = append(dst, v.s...)
		dst = append(dst, '"')
		return dst
	case TypeObject:
		// 对象类型：
		//	∙ 委托给 Object的 MarshalTo方法处理
		return v.o.MarshalTo(dst)
	case TypeArray:
		// 数组类型：
		//	∙ 添加左方括号 [
		//	∙ 遍历数组元素，递归调用每个元素的 MarshalTo
		//	∙ 在元素间添加逗号分隔符
		//	∙ 最后添加右方括号 ]
		dst = append(dst, '[')
		for i, vv := range v.a {
			dst = vv.MarshalTo(dst)
			if i != len(v.a)-1 {
				dst = append(dst, ',')
			}
		}
		dst = append(dst, ']')
		return dst
	case TypeString:
		// 字符串类型：
		//  ∙ 调用 escapeString 对字符串进行转义处理
		return escapeString(dst, v.s)
	case TypeNumber:
		// 数字类型：
		//	∙ 直接将数字字符串表示追加到结果中
		return append(dst, v.s...)
	case TypeTrue:
		return append(dst, "true"...)
	case TypeFalse:
		return append(dst, "false"...)
	case TypeNull:
		return append(dst, "null"...)
	default:
		panic(fmt.Errorf("BUG: unexpected Value type: %d", v.t))
	}
}

// String returns string representation of the v.
//
// The function is for debugging purposes only. It isn't optimized for speed.
// See MarshalTo instead.
//
// Don't confuse this function with StringBytes, which must be called
// for obtaining the underlying JSON string for the v.
func (v *Value) String() string {
	b := v.MarshalTo(nil)
	// It is safe converting b to string without allocation, since b is no longer
	// reachable after this line.
	return b2s(b)
}

// Type represents JSON type.
type Type int

const (
	// TypeNull is JSON null.
	TypeNull Type = 0

	// TypeObject is JSON object type.
	TypeObject Type = 1

	// TypeArray is JSON array type.
	TypeArray Type = 2

	// TypeString is JSON string type.
	TypeString Type = 3

	// TypeNumber is JSON number type.
	TypeNumber Type = 4

	// TypeTrue is JSON true.
	TypeTrue Type = 5

	// TypeFalse is JSON false.
	TypeFalse Type = 6

	typeRawString Type = 7
)

// String returns string representation of t.
func (t Type) String() string {
	switch t {
	case TypeObject:
		return "object"
	case TypeArray:
		return "array"
	case TypeString:
		return "string"
	case TypeNumber:
		return "number"
	case TypeTrue:
		return "true"
	case TypeFalse:
		return "false"
	case TypeNull:
		return "null"

	// typeRawString is skipped intentionally,
	// since it shouldn't be visible to user.
	default:
		panic(fmt.Errorf("BUG: unknown Value type: %d", t))
	}
}

// Type returns the type of the v.
func (v *Value) Type() Type {
	if v.t == typeRawString {
		v.s = unescapeStringBestEffort(v.s)
		v.t = TypeString
	}
	return v.t
}

// Exists returns true if the field exists for the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
func (v *Value) Exists(keys ...string) bool {
	v = v.Get(keys...)
	return v != nil
}

// Get returns value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// nil is returned for non-existing keys path.
//
// The returned value is valid until Parse is called on the Parser returned v.
func (v *Value) Get(keys ...string) *Value {
	if v == nil {
		return nil
	}
	// 按路径查询，逐层深入访问
	for _, key := range keys {
		if v.t == TypeObject {
			// 如果是对象，调用对象自己的 Get 方法查找键对应的值，找不到返回 nil
			v = v.o.Get(key)
			if v == nil {
				return nil
			}
		} else if v.t == TypeArray {
			// 如果是数组，将键转换为数组索引，返回对应元素
			n, err := strconv.Atoi(key)
			if err != nil || n < 0 || n >= len(v.a) {
				return nil
			}
			v = v.a[n]
		} else {
			// 其它类型，返回 nil
			return nil
		}
	}
	// 返回最终找到的 Value
	return v
}

// GetObject returns object value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// nil is returned for non-existing keys path or for invalid value type.
//
// The returned object is valid until Parse is called on the Parser returned v.
func (v *Value) GetObject(keys ...string) *Object {
	v = v.Get(keys...)
	if v == nil || v.t != TypeObject {
		return nil
	}
	return &v.o
}

// GetArray returns array value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// nil is returned for non-existing keys path or for invalid value type.
//
// The returned array is valid until Parse is called on the Parser returned v.
func (v *Value) GetArray(keys ...string) []*Value {
	v = v.Get(keys...)
	if v == nil || v.t != TypeArray {
		return nil
	}
	return v.a
}

// GetFloat64 returns float64 value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// 0 is returned for non-existing keys path or for invalid value type.
func (v *Value) GetFloat64(keys ...string) float64 {
	v = v.Get(keys...)
	if v == nil || v.Type() != TypeNumber {
		return 0
	}
	return fastfloat.ParseBestEffort(v.s)
}

// GetInt returns int value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// 0 is returned for non-existing keys path or for invalid value type.
func (v *Value) GetInt(keys ...string) int {
	v = v.Get(keys...)
	if v == nil || v.Type() != TypeNumber {
		return 0
	}
	n := fastfloat.ParseInt64BestEffort(v.s)
	nn := int(n)
	if int64(nn) != n {
		return 0
	}
	return nn
}

// GetUint returns uint value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// 0 is returned for non-existing keys path or for invalid value type.
func (v *Value) GetUint(keys ...string) uint {
	v = v.Get(keys...)
	if v == nil || v.Type() != TypeNumber {
		return 0
	}
	n := fastfloat.ParseUint64BestEffort(v.s)
	nn := uint(n)
	if uint64(nn) != n {
		return 0
	}
	return nn
}

// GetInt64 returns int64 value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// 0 is returned for non-existing keys path or for invalid value type.
func (v *Value) GetInt64(keys ...string) int64 {
	v = v.Get(keys...)
	if v == nil || v.Type() != TypeNumber {
		return 0
	}
	return fastfloat.ParseInt64BestEffort(v.s)
}

// GetUint64 returns uint64 value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// 0 is returned for non-existing keys path or for invalid value type.
func (v *Value) GetUint64(keys ...string) uint64 {
	v = v.Get(keys...)
	if v == nil || v.Type() != TypeNumber {
		return 0
	}
	return fastfloat.ParseUint64BestEffort(v.s)
}

// GetStringBytes returns string value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// nil is returned for non-existing keys path or for invalid value type.
//
// The returned string is valid until Parse is called on the Parser returned v.
func (v *Value) GetStringBytes(keys ...string) []byte {
	v = v.Get(keys...)
	if v == nil || v.Type() != TypeString {
		return nil
	}
	return s2b(v.s)
}

// GetBool returns bool value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// false is returned for non-existing keys path or for invalid value type.
func (v *Value) GetBool(keys ...string) bool {
	v = v.Get(keys...)
	if v != nil && v.t == TypeTrue {
		return true
	}
	return false
}

// Object returns the underlying JSON object for the v.
//
// The returned object is valid until Parse is called on the Parser returned v.
//
// Use GetObject if you don't need error handling.
func (v *Value) Object() (*Object, error) {
	if v.t != TypeObject {
		return nil, fmt.Errorf("value doesn't contain object; it contains %s", v.Type())
	}
	return &v.o, nil
}

// Array returns the underlying JSON array for the v.
//
// The returned array is valid until Parse is called on the Parser returned v.
//
// Use GetArray if you don't need error handling.
func (v *Value) Array() ([]*Value, error) {
	if v.t != TypeArray {
		return nil, fmt.Errorf("value doesn't contain array; it contains %s", v.Type())
	}
	return v.a, nil
}

// StringBytes returns the underlying JSON string for the v.
//
// The returned string is valid until Parse is called on the Parser returned v.
//
// Use GetStringBytes if you don't need error handling.
func (v *Value) StringBytes() ([]byte, error) {
	if v.Type() != TypeString {
		return nil, fmt.Errorf("value doesn't contain string; it contains %s", v.Type())
	}
	return s2b(v.s), nil
}

// Float64 returns the underlying JSON number for the v.
//
// Use GetFloat64 if you don't need error handling.
func (v *Value) Float64() (float64, error) {
	if v.Type() != TypeNumber {
		return 0, fmt.Errorf("value doesn't contain number; it contains %s", v.Type())
	}
	return fastfloat.Parse(v.s)
}

// Int returns the underlying JSON int for the v.
//
// Use GetInt if you don't need error handling.
func (v *Value) Int() (int, error) {
	if v.Type() != TypeNumber {
		return 0, fmt.Errorf("value doesn't contain number; it contains %s", v.Type())
	}
	n, err := fastfloat.ParseInt64(v.s)
	if err != nil {
		return 0, err
	}
	nn := int(n)
	if int64(nn) != n {
		return 0, fmt.Errorf("number %q doesn't fit int", v.s)
	}
	return nn, nil
}

// Uint returns the underlying JSON uint for the v.
//
// Use GetInt if you don't need error handling.
func (v *Value) Uint() (uint, error) {
	if v.Type() != TypeNumber {
		return 0, fmt.Errorf("value doesn't contain number; it contains %s", v.Type())
	}
	n, err := fastfloat.ParseUint64(v.s)
	if err != nil {
		return 0, err
	}
	nn := uint(n)
	if uint64(nn) != n {
		return 0, fmt.Errorf("number %q doesn't fit uint", v.s)
	}
	return nn, nil
}

// Int64 returns the underlying JSON int64 for the v.
//
// Use GetInt64 if you don't need error handling.
func (v *Value) Int64() (int64, error) {
	if v.Type() != TypeNumber {
		return 0, fmt.Errorf("value doesn't contain number; it contains %s", v.Type())
	}
	return fastfloat.ParseInt64(v.s)
}

// Uint64 returns the underlying JSON uint64 for the v.
//
// Use GetInt64 if you don't need error handling.
func (v *Value) Uint64() (uint64, error) {
	if v.Type() != TypeNumber {
		return 0, fmt.Errorf("value doesn't contain number; it contains %s", v.Type())
	}
	return fastfloat.ParseUint64(v.s)
}

// Bool returns the underlying JSON bool for the v.
//
// Use GetBool if you don't need error handling.
func (v *Value) Bool() (bool, error) {
	if v.t == TypeTrue {
		return true, nil
	}
	if v.t == TypeFalse {
		return false, nil
	}
	return false, fmt.Errorf("value doesn't contain bool; it contains %s", v.Type())
}

var (
	valueTrue  = &Value{t: TypeTrue}
	valueFalse = &Value{t: TypeFalse}
	valueNull  = &Value{t: TypeNull}
)
