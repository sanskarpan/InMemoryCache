package list

// Node is a node in a doubly-linked list.
type Node[T any] struct {
	Value T
	prev  *Node[T]
	next  *Node[T]
}

func (n *Node[T]) Prev() *Node[T] { return n.prev }
func (n *Node[T]) Next() *Node[T] { return n.next }

// List is a generic doubly-linked list with O(1) move-to-front and removal.
type List[T any] struct {
	head *Node[T]
	tail *Node[T]
	len  int
}

// New creates a new empty list.
func New[T any]() *List[T] {
	return &List[T]{}
}

// Len returns the number of nodes in the list.
func (l *List[T]) Len() int { return l.len }

// Front returns the head node (MRU position).
func (l *List[T]) Front() *Node[T] { return l.head }

// Back returns the tail node (LRU position).
func (l *List[T]) Back() *Node[T] { return l.tail }

// PushFront inserts value at the front and returns the new node.
func (l *List[T]) PushFront(value T) *Node[T] {
	n := &Node[T]{Value: value}
	if l.head == nil {
		l.head = n
		l.tail = n
	} else {
		n.next = l.head
		l.head.prev = n
		l.head = n
	}
	l.len++
	return n
}

// PushBack inserts value at the back and returns the new node.
func (l *List[T]) PushBack(value T) *Node[T] {
	n := &Node[T]{Value: value}
	if l.tail == nil {
		l.head = n
		l.tail = n
	} else {
		n.prev = l.tail
		l.tail.next = n
		l.tail = n
	}
	l.len++
	return n
}

// MoveToFront moves the given node to the front of the list.
func (l *List[T]) MoveToFront(n *Node[T]) {
	if n == l.head || l.len <= 1 {
		return
	}

	// Detach node from its current position
	if n == l.tail {
		// Node is the tail — update tail pointer
		l.tail = n.prev
		l.tail.next = nil
	} else {
		// Node is in the middle
		n.prev.next = n.next
		n.next.prev = n.prev
	}

	// Insert at front
	n.prev = nil
	n.next = l.head
	l.head.prev = n
	l.head = n
}

// Remove removes the given node from the list.
func (l *List[T]) Remove(n *Node[T]) {
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		l.head = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		l.tail = n.prev
	}
	n.prev = nil
	n.next = nil
	l.len--
}

// ToSlice returns all values from front to back.
func (l *List[T]) ToSlice() []T {
	result := make([]T, 0, l.len)
	cur := l.head
	for cur != nil {
		result = append(result, cur.Value)
		cur = cur.next
	}
	return result
}
