package fastjson

import (
	"strconv"
	"strings"
)

// Del deletes the entry with the given key from o.
// 删除 Object 中的键
func (o *Object) Del(key string) {
	if o == nil {
		return
	}

	// 快速路径：键未转义且要删除的键不包含反斜杠，直接在 o.kvs 里查找目标字符串，找到就 append(o.kvs[:i], o.kvs[i+1:]...) 删除。
	if !o.keysUnescaped && strings.IndexByte(key, '\\') < 0 {
		// Fast path - try searching for the key without object keys unescaping.
		for i, kv := range o.kvs {
			if kv.k == key {
				o.kvs = append(o.kvs[:i], o.kvs[i+1:]...)
				return
			}
		}
	}

	// Slow path - unescape object keys before item search.
	// 先转义所有键，然后再查找删除
	o.unescapeKeys()
	for i, kv := range o.kvs {
		if kv.k == key {
			o.kvs = append(o.kvs[:i], o.kvs[i+1:]...)
			return
		}
	}
}

// Del deletes the entry with the given key from array or object v.
//
// 通用删除方法
func (v *Value) Del(key string) {
	if v == nil {
		return
	}

	// 对象
	if v.t == TypeObject {
		v.o.Del(key) // 按键删除
		return
	}
	// 数组
	if v.t == TypeArray {
		n, err := strconv.Atoi(key) // 按索引删除
		if err != nil || n < 0 || n >= len(v.a) {
			return
		}
		v.a = append(v.a[:n], v.a[n+1:]...)
	}
}

// Set sets (key, value) entry in the o.
//
// The value must be unchanged during o lifetime.
//
// 设置对象键值
func (o *Object) Set(key string, value *Value) {
	if o == nil {
		return
	}
	if value == nil {
		value = valueNull
	}

	// 确保键已转义，因为后续要做键的查找（匹配）
	o.unescapeKeys()

	// Try substituting already existing entry with the given key.
	// 先尝试更新已存在的键
	for i := range o.kvs {
		kv := &o.kvs[i]
		if kv.k == key {
			kv.v = value // 更新值
			return
		}
	}

	// Add new entry.
	// 添加新键值对
	kv := o.getKV() // 从缓存获取新的 kv 对象
	kv.k = key
	kv.v = value
}

// Set sets (key, value) entry in the array or object v.
//
// The value must be unchanged during v lifetime.
func (v *Value) Set(key string, value *Value) {
	if v == nil {
		return
	}
	if v.t == TypeObject {
		v.o.Set(key, value)
		return
	}
	if v.t == TypeArray {
		idx, err := strconv.Atoi(key)
		if err != nil || idx < 0 {
			return
		}
		v.SetArrayItem(idx, value)
	}
}

// SetArrayItem sets the value in the array v at idx position.
//
// The value must be unchanged during v lifetime.
func (v *Value) SetArrayItem(idx int, value *Value) {
	if v == nil || v.t != TypeArray {
		return
	}
	// 自动扩展数组大小
	for idx >= len(v.a) {
		v.a = append(v.a, valueNull) // 用 null 填充空缺
	}
	// 设置指定位置的值
	v.a[idx] = value
}
