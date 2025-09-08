package fastfloat

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseUint64BestEffort parses uint64 number s.
//
// It is equivalent to strconv.ParseUint(s, 10, 64), but is faster.
//
// 0 is returned if the number cannot be parsed.
// See also ParseUint64, which returns parse error if the number cannot be parsed.
func ParseUint64BestEffort(s string) uint64 {
	if len(s) == 0 {
		return 0
	}
	i := uint(0)
	d := uint64(0)
	j := i
	for i < uint(len(s)) {
		if s[i] >= '0' && s[i] <= '9' {
			d = d*10 + uint64(s[i]-'0')
			i++
			if i > 18 {
				// The integer part may be out of range for uint64.
				// Fall back to slow parsing.
				dd, err := strconv.ParseUint(s, 10, 64)
				if err != nil {
					return 0
				}
				return dd
			}
			continue
		}
		break
	}
	if i <= j {
		return 0
	}
	if i < uint(len(s)) {
		// Unparsed tail left.
		return 0
	}
	return d
}

// ParseUint64 parses uint64 from s.
//
// It is equivalent to strconv.ParseUint(s, 10, 64), but is faster.
//
// See also ParseUint64BestEffort.
func ParseUint64(s string) (uint64, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("cannot parse uint64 from empty string")
	}
	i := uint(0)
	d := uint64(0)
	j := i
	for i < uint(len(s)) {
		if s[i] >= '0' && s[i] <= '9' {
			d = d*10 + uint64(s[i]-'0')
			i++
			if i > 18 {
				// The integer part may be out of range for uint64.
				// Fall back to slow parsing.
				dd, err := strconv.ParseUint(s, 10, 64)
				if err != nil {
					return 0, err
				}
				return dd, nil
			}
			continue
		}
		break
	}
	if i <= j {
		return 0, fmt.Errorf("cannot parse uint64 from %q", s)
	}
	if i < uint(len(s)) {
		// Unparsed tail left.
		return 0, fmt.Errorf("unparsed tail left after parsing uint64 from %q: %q", s, s[i:])
	}
	return d, nil
}

// ParseInt64BestEffort parses int64 number s.
//
// It is equivalent to strconv.ParseInt(s, 10, 64), but is faster.
//
// 0 is returned if the number cannot be parsed.
// See also ParseInt64, which returns parse error if the number cannot be parsed.
func ParseInt64BestEffort(s string) int64 {
	if len(s) == 0 {
		return 0
	}
	i := uint(0)
	minus := s[0] == '-'
	if minus {
		i++
		if i >= uint(len(s)) {
			return 0
		}
	}

	d := int64(0)
	j := i
	for i < uint(len(s)) {
		if s[i] >= '0' && s[i] <= '9' {
			d = d*10 + int64(s[i]-'0')
			i++
			if i > 18 {
				// The integer part may be out of range for int64.
				// Fall back to slow parsing.
				dd, err := strconv.ParseInt(s, 10, 64)
				if err != nil {
					return 0
				}
				return dd
			}
			continue
		}
		break
	}
	if i <= j {
		return 0
	}
	if i < uint(len(s)) {
		// Unparsed tail left.
		return 0
	}
	if minus {
		d = -d
	}
	return d
}

// ParseInt64 parses int64 number s.
//
// It is equivalent to strconv.ParseInt(s, 10, 64), but is faster.
//
// See also ParseInt64BestEffort.
func ParseInt64(s string) (int64, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("cannot parse int64 from empty string")
	}
	i := uint(0)
	minus := s[0] == '-'
	if minus {
		i++
		if i >= uint(len(s)) {
			return 0, fmt.Errorf("cannot parse int64 from %q", s)
		}
	}

	d := int64(0)
	j := i
	for i < uint(len(s)) {
		if s[i] >= '0' && s[i] <= '9' {
			d = d*10 + int64(s[i]-'0')
			i++
			if i > 18 {
				// The integer part may be out of range for int64.
				// Fall back to slow parsing.
				dd, err := strconv.ParseInt(s, 10, 64)
				if err != nil {
					return 0, err
				}
				return dd, nil
			}
			continue
		}
		break
	}
	if i <= j {
		return 0, fmt.Errorf("cannot parse int64 from %q", s)
	}
	if i < uint(len(s)) {
		// Unparsed tail left.
		return 0, fmt.Errorf("unparsed tail left after parsing int64 form %q: %q", s, s[i:])
	}
	if minus {
		d = -d
	}
	return d, nil
}

// Exact powers of 10.
//
// This works faster than math.Pow10, since it avoids additional multiplication.
var float64pow10 = [...]float64{
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16,
}

// ParseBestEffort parses floating-point number s.
//
// It is equivalent to strconv.ParseFloat(s, 64), but is faster.
//
// 0 is returned if the number cannot be parsed.
// See also Parse, which returns parse error if the number cannot be parsed.
//
// 特点：
//
//	宽松解析：
//		即使数字不完全符合标准，也尽量解析，即使有轻微格式问题，也不会报错。
//		例如 "." 后面没有数字，或者指数缺失时，返回 0 或当前解析到的部分。
//
//	快速路径：
//		手动累加整数部分到 uint64 d。
//		如果整数部分过大（>18位），直接 fallback 到 strconv.ParseFloat。
//
//	小数部分：
//		解析完整数部分后，如果遇到 .，继续累加到 d。
//		用预先计算好的 float64pow10 做除法，快速得到小数值。
//
//	指数部分：
//		遇到 e 或 E，手动解析指数并用 math.Pow10 调整。
//
//	特殊值：
//		支持 "inf"、"infinity"、"nan"（OpenMetrics 标准）。
func ParseBestEffort(s string) float64 {
	if len(s) == 0 {
		return 0 // 空字符串返回0
	}
	i := uint(0)
	minus := s[0] == '-'
	if minus {
		i++
		if i >= uint(len(s)) {
			return 0 // 只有负号没有数字
		}
	}

	// the integer part might be elided to remain compliant
	// with https://go.dev/ref/spec#Floating-point_literals
	if s[i] == '.' && (i+1 >= uint(len(s)) || s[i+1] < '0' || s[i+1] > '9') {
		return 0 // 无效的小数点格式，如 "." 或 ".abc"
	}

	// 解析整数部分
	d := uint64(0)
	j := i
	for i < uint(len(s)) {
		if s[i] >= '0' && s[i] <= '9' {
			d = d*10 + uint64(s[i]-'0') // 累加数字
			i++
			if i > 18 {
				// The integer part may be out of range for uint64.
				// Fall back to slow parsing.
				//
				// 整数部分太大，回退到标准库
				f, err := strconv.ParseFloat(s, 64)
				if err != nil && !math.IsInf(f, 0) {
					return 0
				}
				return f
			}
			continue
		}
		break
	}

	// 特殊值处理
	if i <= j && s[i] != '.' {
		s = s[i:] // 剩余字符串
		if strings.HasPrefix(s, "+") {
			s = s[1:] // 去掉正号
		}
		// "infinity" is needed for OpenMetrics support.
		// See https://github.com/OpenObservability/OpenMetrics/blob/master/OpenMetrics.md
		//
		// 支持 Infinity 和 NaN
		if strings.EqualFold(s, "inf") || strings.EqualFold(s, "infinity") {
			if minus {
				return -inf // -Inf
			}
			return inf // +Inf
		}
		if strings.EqualFold(s, "nan") {
			return nan // NaN
		}
		return 0 // 不是特殊值
	}

	// 快速路径 - 纯整数
	f := float64(d)
	if i >= uint(len(s)) {
		// Fast path - just integer.
		if minus {
			f = -f // 应用负号
		}
		return f // 没有更多字符，直接返回整数
	}

	// 解析小数部分
	if s[i] == '.' {
		// Parse fractional part.
		i++ // 跳过小数点
		if i >= uint(len(s)) {
			// the fractional part may be elided to remain compliant
			// with https://go.dev/ref/spec#Floating-point_literals
			return f
		}
		k := i // 记录小数部分起始位置
		for i < uint(len(s)) {
			if s[i] >= '0' && s[i] <= '9' {
				d = d*10 + uint64(s[i]-'0') // 继续累加
				i++
				if i-j >= uint(len(float64pow10)) {
					// The mantissa is out of range. Fall back to standard parsing.
					// 数字太长，回退到标准库
					f, err := strconv.ParseFloat(s, 64)
					if err != nil && !math.IsInf(f, 0) {
						return 0
					}
					return f
				}
				continue
			}
			break
		}
		if i < k {
			return 0
		}
		// Convert the entire mantissa to a float at once to avoid rounding errors.
		// 一次性计算整个小数部分，避免累积误差
		f = float64(d) / float64pow10[i-k] // 除以10的n次幂
		if i >= uint(len(s)) {
			// Fast path - parsed fractional number.
			if minus {
				f = -f
			}
			return f // 没有指数部分
		}
	}

	// 解析指数部分
	if s[i] == 'e' || s[i] == 'E' {
		// Parse exponent part.
		i++ // 跳过'e'或'E'
		if i >= uint(len(s)) {
			return 0
		}
		expMinus := false
		if s[i] == '+' || s[i] == '-' {
			expMinus = s[i] == '-' // 指数符号
			i++
			if i >= uint(len(s)) {
				return 0
			}
		}
		exp := int16(0)
		j := i // 记录指数起始位置
		for i < uint(len(s)) {
			if s[i] >= '0' && s[i] <= '9' {
				exp = exp*10 + int16(s[i]-'0')
				i++
				if exp > 300 {
					// The exponent may be too big for float64.
					// Fall back to standard parsing.
					//
					// 指数太大，回退到标准库
					f, err := strconv.ParseFloat(s, 64)
					if err != nil && !math.IsInf(f, 0) {
						return 0
					}
					return f
				}
				continue
			}
			break
		}
		if i <= j {
			return 0
		}
		if expMinus {
			exp = -exp // 负指数
		}
		f *= math.Pow10(int(exp)) // 应用指数
		if i >= uint(len(s)) {
			if minus {
				f = -f
			}
			return f
		}
	}
	return 0
}

// Parse parses floating-point number s.
//
// It is equivalent to strconv.ParseFloat(s, 64), but is faster.
//
// See also ParseBestEffort.
//

func Parse(s string) (float64, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("cannot parse float64 from empty string")
	}
	i := uint(0)
	minus := s[0] == '-'
	if minus {
		i++
		if i >= uint(len(s)) {
			return 0, fmt.Errorf("cannot parse float64 from %q", s)
		}
	}

	// the integer part might be elided to remain compliant
	// with https://go.dev/ref/spec#Floating-point_literals
	if s[i] == '.' && (i+1 >= uint(len(s)) || s[i+1] < '0' || s[i+1] > '9') {
		return 0, fmt.Errorf("missing integer and fractional part in %q", s)
	}

	d := uint64(0)
	j := i
	for i < uint(len(s)) {
		if s[i] >= '0' && s[i] <= '9' {
			d = d*10 + uint64(s[i]-'0')
			i++
			if i > 18 {
				// The integer part may be out of range for uint64.
				// Fall back to slow parsing.
				f, err := strconv.ParseFloat(s, 64)
				if err != nil && !math.IsInf(f, 0) {
					return 0, err
				}
				return f, nil
			}
			continue
		}
		break
	}
	if i <= j && s[i] != '.' {
		ss := s[i:]
		if strings.HasPrefix(ss, "+") {
			ss = ss[1:]
		}
		// "infinity" is needed for OpenMetrics support.
		// See https://github.com/OpenObservability/OpenMetrics/blob/master/OpenMetrics.md
		if strings.EqualFold(ss, "inf") || strings.EqualFold(ss, "infinity") {
			if minus {
				return -inf, nil
			}
			return inf, nil
		}
		if strings.EqualFold(ss, "nan") {
			return nan, nil
		}
		return 0, fmt.Errorf("unparsed tail left after parsing float64 from %q: %q", s, ss)
	}
	f := float64(d)
	if i >= uint(len(s)) {
		// Fast path - just integer.
		if minus {
			f = -f
		}
		return f, nil
	}

	if s[i] == '.' {
		// Parse fractional part.
		i++
		if i >= uint(len(s)) {
			// the fractional part might be elided to remain compliant
			// with https://go.dev/ref/spec#Floating-point_literals
			return f, nil
		}
		k := i
		for i < uint(len(s)) {
			if s[i] >= '0' && s[i] <= '9' {
				d = d*10 + uint64(s[i]-'0')
				i++
				if i-j >= uint(len(float64pow10)) {
					// The mantissa is out of range. Fall back to standard parsing.
					f, err := strconv.ParseFloat(s, 64)
					if err != nil && !math.IsInf(f, 0) {
						return 0, fmt.Errorf("cannot parse mantissa in %q: %s", s, err)
					}
					return f, nil
				}
				continue
			}
			break
		}
		if i < k {
			return 0, fmt.Errorf("cannot find mantissa in %q", s)
		}
		// Convert the entire mantissa to a float at once to avoid rounding errors.
		f = float64(d) / float64pow10[i-k]
		if i >= uint(len(s)) {
			// Fast path - parsed fractional number.
			if minus {
				f = -f
			}
			return f, nil
		}
	}
	if s[i] == 'e' || s[i] == 'E' {
		// Parse exponent part.
		i++
		if i >= uint(len(s)) {
			return 0, fmt.Errorf("cannot parse exponent in %q", s)
		}
		expMinus := false
		if s[i] == '+' || s[i] == '-' {
			expMinus = s[i] == '-'
			i++
			if i >= uint(len(s)) {
				return 0, fmt.Errorf("cannot parse exponent in %q", s)
			}
		}
		exp := int16(0)
		j := i
		for i < uint(len(s)) {
			if s[i] >= '0' && s[i] <= '9' {
				exp = exp*10 + int16(s[i]-'0')
				i++
				if exp > 300 {
					// The exponent may be too big for float64.
					// Fall back to standard parsing.
					f, err := strconv.ParseFloat(s, 64)
					if err != nil && !math.IsInf(f, 0) {
						return 0, fmt.Errorf("cannot parse exponent in %q: %s", s, err)
					}
					return f, nil
				}
				continue
			}
			break
		}
		if i <= j {
			return 0, fmt.Errorf("cannot parse exponent in %q", s)
		}
		if expMinus {
			exp = -exp
		}
		f *= math.Pow10(int(exp))
		if i >= uint(len(s)) {
			if minus {
				f = -f
			}
			return f, nil
		}
	}
	return 0, fmt.Errorf("cannot parse float64 from %q", s)
}

var inf = math.Inf(1)
var nan = math.NaN()
