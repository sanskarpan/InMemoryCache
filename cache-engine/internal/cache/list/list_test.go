package list

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func checkInvariants(t *testing.T, l *List[int]) {
	t.Helper()
	if l.head != nil {
		assert.Nil(t, l.head.prev, "head.prev must be nil")
	}
	if l.tail != nil {
		assert.Nil(t, l.tail.next, "tail.next must be nil")
	}
	count := 0
	cur := l.head
	for cur != nil {
		count++
		cur = cur.next
	}
	assert.Equal(t, l.Len(), count, "Len() must match actual node count")
}

func TestList_PushFront(t *testing.T) {
	l := New[int]()
	n1 := l.PushFront(1)
	checkInvariants(t, l)
	assert.Equal(t, 1, l.Len())
	assert.Equal(t, n1, l.Front())
	assert.Equal(t, n1, l.Back())

	n2 := l.PushFront(2)
	checkInvariants(t, l)
	assert.Equal(t, 2, l.Len())
	assert.Equal(t, n2, l.Front())
	assert.Equal(t, n1, l.Back())
}

func TestList_PushBack(t *testing.T) {
	l := New[int]()
	n1 := l.PushBack(1)
	checkInvariants(t, l)
	n2 := l.PushBack(2)
	checkInvariants(t, l)
	assert.Equal(t, 2, l.Len())
	assert.Equal(t, n1, l.Front())
	assert.Equal(t, n2, l.Back())
}

func TestList_MoveToFront_AlreadyHead(t *testing.T) {
	l := New[int]()
	n1 := l.PushBack(1)
	l.PushBack(2)
	l.MoveToFront(n1)
	checkInvariants(t, l)
	assert.Equal(t, n1, l.Front())
}

func TestList_MoveToFront_Tail(t *testing.T) {
	l := New[int]()
	l.PushBack(1)
	n2 := l.PushBack(2)
	l.MoveToFront(n2)
	checkInvariants(t, l)
	assert.Equal(t, n2, l.Front())
	assert.Equal(t, 2, l.Len())
}

func TestList_MoveToFront_Middle(t *testing.T) {
	l := New[int]()
	l.PushBack(1)
	n2 := l.PushBack(2)
	l.PushBack(3)
	l.MoveToFront(n2)
	checkInvariants(t, l)
	assert.Equal(t, n2, l.Front())
	assert.Equal(t, 3, l.Len())
	assert.Equal(t, []int{2, 1, 3}, l.ToSlice())
}

func TestList_MoveToFront_SingleElement(t *testing.T) {
	l := New[int]()
	n1 := l.PushBack(1)
	l.MoveToFront(n1)
	checkInvariants(t, l)
	assert.Equal(t, 1, l.Len())
}

func TestList_Remove_Head(t *testing.T) {
	l := New[int]()
	n1 := l.PushBack(1)
	l.PushBack(2)
	l.Remove(n1)
	checkInvariants(t, l)
	assert.Equal(t, 1, l.Len())
}

func TestList_Remove_Tail(t *testing.T) {
	l := New[int]()
	l.PushBack(1)
	n2 := l.PushBack(2)
	l.Remove(n2)
	checkInvariants(t, l)
	assert.Equal(t, 1, l.Len())
}

func TestList_Remove_Middle(t *testing.T) {
	l := New[int]()
	l.PushBack(1)
	n2 := l.PushBack(2)
	l.PushBack(3)
	l.Remove(n2)
	checkInvariants(t, l)
	assert.Equal(t, 2, l.Len())
	assert.Equal(t, []int{1, 3}, l.ToSlice())
}

func TestList_Remove_Only(t *testing.T) {
	l := New[int]()
	n1 := l.PushBack(1)
	l.Remove(n1)
	checkInvariants(t, l)
	assert.Equal(t, 0, l.Len())
	assert.Nil(t, l.Front())
	assert.Nil(t, l.Back())
}

func TestList_ToSlice(t *testing.T) {
	l := New[int]()
	l.PushBack(1)
	l.PushBack(2)
	l.PushBack(3)
	checkInvariants(t, l)
	assert.Equal(t, []int{1, 2, 3}, l.ToSlice())
}

func TestList_EmptyOperations(t *testing.T) {
	l := New[int]()
	assert.Nil(t, l.Front())
	assert.Nil(t, l.Back())
	assert.Equal(t, 0, l.Len())
	assert.Equal(t, []int{}, l.ToSlice())
}
