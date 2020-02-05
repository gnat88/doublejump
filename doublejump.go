// Package doublejump provides a revamped Google's jump consistent hash.
package doublejump

import (
	"sync"

	"github.com/dgryski/go-jump"
)

// 保持全量的节点信息，删除节点的时候不会从数组中直接删除，需要保留位置，将该位置对应的节点设置为nil
// 增加节点的时候优先往空位置中填放
type looseHolder struct {
	a []interface{}
	m map[interface{}]int
	emptyPoses []int
}

func (this *looseHolder) add(obj interface{}) {
	if _, ok := this.m[obj]; ok {
		return
	}

	if nf := len(this.emptyPoses); nf == 0 {
		this.a = append(this.a, obj)
		this.m[obj] = len(this.a) - 1
	} else {
		idx := this.emptyPoses[nf-1]
		this.emptyPoses = this.emptyPoses[:nf-1] // 取出最后一个空位置，用于存放新节点
		this.a[idx] = obj
		this.m[obj] = idx
	}
}

// 删除节点: 标记删除节点的位置为空
func (this *looseHolder) remove(obj interface{}) {
	if idx, ok := this.m[obj]; ok {
		this.emptyPoses = append(this.emptyPoses, idx)
		this.a[idx] = nil
		delete(this.m, obj)
	}
}

// 根据KEY计算一致性哈希值
// 由于哈希桶的数量取的是全量的数据，所以如果哈希到已经删除的节点，会返回空
func (this *looseHolder) get(key uint64) interface{} {
	na := len(this.a)
	if na == 0 {
		return nil
	}

	h := jump.Hash(key, na)
	return this.a[h]
}

func (this *looseHolder) shrink() {
	if len(this.emptyPoses) == 0 {
		return
	}

	var a []interface{}
	for _, obj := range this.a {
		if obj != nil {
			a = append(a, obj)
			this.m[obj] = len(a) - 1
		}
	}
	this.a = a
	this.emptyPoses = nil
}

// 作为looseHolder的"候补"，保存着当前有效的节点信息，不存在空位置
// 当looseHolder哈希出来的值是已经删除的节点，就需要通过compactHolder重新计算一次
type compactHolder struct {
	a []interface{}
	m map[interface{}]int
}

func (this *compactHolder) add(obj interface{}) {
	if _, ok := this.m[obj]; ok {
		return
	}

	this.a = append(this.a, obj)
	this.m[obj] = len(this.a) - 1
}

func (this *compactHolder) shrink(a []interface{}) {
	for i, obj := range a {
		this.a[i] = obj
		this.m[obj] = i
	}
}

// 删除节点后，将当前最后的节点放到空位置中, 然后再将数组长度缩减1位
func (this *compactHolder) remove(obj interface{}) {
	if idx, ok := this.m[obj]; ok {
		n := len(this.a)
		this.a[idx] = this.a[n-1]
		this.m[this.a[idx]] = idx
		this.a[n-1] = nil
		this.a = this.a[:n-1]
		delete(this.m, obj)
	}
}

func (this *compactHolder) get(key uint64) interface{} {
	na := len(this.a)
	if na == 0 {
		return nil
	}

	// 这里大概是将KEY变换一下？
	h := jump.Hash(key*0xc6a4a7935bd1e995, na)
	return this.a[h]
}

// Hash is a revamped Google's jump consistent hash. It overcomes the shortcoming of the
// original implementation - not being able to remove nodes.
type Hash struct {
	mu      sync.RWMutex
	loose   looseHolder
	compact compactHolder
	lock    bool
}

// NewHash creates a new doublejump hash instance, which is threadsafe.
func NewHash() *Hash {
	hash := &Hash{lock: true}
	hash.loose.m = make(map[interface{}]int)
	hash.compact.m = make(map[interface{}]int)
	return hash
}

// NewHashWithoutLock creates a new doublejump hash instance, which does NOT threadsafe.
func NewHashWithoutLock() *Hash {
	hash := &Hash{}
	hash.loose.m = make(map[interface{}]int)
	hash.compact.m = make(map[interface{}]int)
	return hash
}

// Add adds an object to the hash.
func (this *Hash) Add(obj interface{}) {
	if this == nil || obj == nil {
		return
	}

	if this.lock {
		this.mu.Lock()
		defer this.mu.Unlock()
	}

	this.loose.add(obj)
	this.compact.add(obj)
}

// Remove removes an object from the hash.
func (this *Hash) Remove(obj interface{}) {
	if this == nil || obj == nil {
		return
	}

	if this.lock {
		this.mu.Lock()
		defer this.mu.Unlock()
	}

	this.loose.remove(obj)
	this.compact.remove(obj)
}

// Len returns the number of objects in the hash.
func (this *Hash) Len() int {
	if this == nil {
		return 0
	}

	if this.lock {
		this.mu.RLock()
		n := len(this.compact.a)
		this.mu.RUnlock()
		return n
	}

	return len(this.compact.a)
}

// LooseLen returns the size of the inner loose object holder.
func (this *Hash) LooseLen() int {
	if this == nil {
		return 0
	}

	if this.lock {
		this.mu.RLock()
		n := len(this.loose.a)
		this.mu.RUnlock()
		return n
	}

	return len(this.loose.a)
}

// Shrink removes all empty slots from the hash.
func (this *Hash) Shrink() {
	if this == nil {
		return
	}

	if this.lock {
		this.mu.Lock()
		defer this.mu.Unlock()
	}

	this.loose.shrink()
	this.compact.shrink(this.loose.a)
}

// Get returns an object according to the key provided.
func (this *Hash) Get(key uint64) interface{} {
	if this == nil {
		return nil
	}

	var obj interface{}
	if this.lock {
		this.mu.RLock()
		obj = this.loose.get(key)
		switch obj {
		case nil:
			obj = this.compact.get(key)
		}
		this.mu.RUnlock()
	} else {
		obj = this.loose.get(key)
		switch obj {
		case nil:
			obj = this.compact.get(key)
		}
	}

	return obj
}
