package app

import "container/list"

// lruCache is a fixed-capacity least-recently-used cache.
type lruCache[K comparable, V any] struct {
	cap   int
	items map[K]*list.Element
	order *list.List
}

type lruEntry[K comparable, V any] struct {
	key K
	val V
}

func newLRU[K comparable, V any](capacity int) *lruCache[K, V] {
	if capacity < 1 {
		capacity = 1
	}
	return &lruCache[K, V]{
		cap:   capacity,
		items: make(map[K]*list.Element, capacity),
		order: list.New(),
	}
}

func (c *lruCache[K, V]) Get(k K) (V, bool) {
	if c == nil {
		var zero V
		return zero, false
	}
	el, ok := c.items[k]
	if !ok {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*lruEntry[K, V]).val, true
}

func (c *lruCache[K, V]) Put(k K, v V) {
	if c == nil {
		return
	}
	if el, ok := c.items[k]; ok {
		el.Value.(*lruEntry[K, V]).val = v
		c.order.MoveToFront(el)
		return
	}
	if c.order.Len() >= c.cap {
		tail := c.order.Back()
		if tail != nil {
			c.order.Remove(tail)
			delete(c.items, tail.Value.(*lruEntry[K, V]).key)
		}
	}
	el := c.order.PushFront(&lruEntry[K, V]{key: k, val: v})
	c.items[k] = el
}

func (c *lruCache[K, V]) Delete(k K) {
	if c == nil {
		return
	}
	if el, ok := c.items[k]; ok {
		c.order.Remove(el)
		delete(c.items, k)
	}
}

func (c *lruCache[K, V]) DeleteFunc(fn func(K, V) bool) {
	if c == nil {
		return
	}
	for el := c.order.Front(); el != nil; {
		next := el.Next()
		ent := el.Value.(*lruEntry[K, V])
		if fn(ent.key, ent.val) {
			c.order.Remove(el)
			delete(c.items, ent.key)
		}
		el = next
	}
}

func (c *lruCache[K, V]) Clear() {
	if c == nil {
		return
	}
	clear(c.items)
	c.order.Init()
}

func (c *lruCache[K, V]) Len() int {
	if c == nil {
		return 0
	}
	return len(c.items)
}
