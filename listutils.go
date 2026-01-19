package glisp

import (
	"errors"
	"fmt"
	"log"
)

var NotAList = errors.New("not a list")

func ListToArray(expr Sexp) ([]Sexp, error) {
	if !IsList(expr) {
		return nil, NotAList
	}
	arr := make([]Sexp, 0)

	for expr != SexpNull {
		list := expr.(SexpPair)
		arr = append(arr, list.head)
		expr = list.tail
	}

	return arr, nil
}

func MakeList(expressions []Sexp) Sexp {
	if len(expressions) == 0 {
		return SexpNull
	}

	return Cons(expressions[0], MakeList(expressions[1:]))
}

func AppendList(list Sexp, adds []Sexp) Sexp {
	if !IsList(list) {
		log.Fatalf("AppendList must be called with a SexpPair type, given %v(%T)", list, list)
	}

	count := 0

	for cur := list; cur != SexpNull; cur = cur.(SexpPair).Tail() {
		count++
	}

	cargs := make([]Sexp, 0, count+len(adds))

	for cur := list; cur != SexpNull; cur = cur.(SexpPair).Tail() {
		cargs = append(cargs, cur.(SexpPair).Head())
	}

	cargs = append(cargs, adds...)

	ret := MakeList(cargs)

	return ret
}

func FoldrPair(env *Glisp, fun SexpFunction, expr Sexp, acc Sexp) (Sexp, error) {
	var err error

	var fnApplyLast func(Sexp, Sexp) (Sexp, error)

	fnApplyLast = func(e Sexp, acc Sexp) (Sexp, error) {
		switch pair := e.(type) {
		case SexpPair:
			acc, err = fnApplyLast(pair.tail, acc)
			if err != nil {
				return acc, err
			}
			return env.Apply(fun, []Sexp{pair.head, acc})
		}
		if e == SexpNull {
			return acc, nil
		}
		return env.Apply(fun, []Sexp{e, acc})
	}

	return fnApplyLast(expr, acc)
}

func FoldlPair(env *Glisp, fun SexpFunction, expr Sexp, acc Sexp) (Sexp, error) {
	var err error

	cur := expr

	for {
		switch pair := cur.(type) {
		case SexpPair:
			acc, err = env.Apply(fun, []Sexp{pair.head, acc})
			if err != nil {
				return acc, err
			}
			cur = pair.tail
		default:
			if pair == SexpNull {
				return acc, nil
			}
			return env.Apply(fun, []Sexp{cur, acc})
		}

	}
}

func MapList(env *Glisp, fun SexpFunction, expr Sexp) (Sexp, error) {
	if expr == SexpNull {
		return SexpNull, nil
	}

	var list SexpPair
	switch e := expr.(type) {
	case SexpPair:
		list = e
	default:
		return SexpNull, NotAList
	}

	var err error

	list.head, err = env.Apply(fun, []Sexp{list.head})

	if err != nil {
		return SexpNull, err
	}

	list.tail, err = MapList(env, fun, list.tail)

	if err != nil {
		return SexpNull, err
	}

	return list, nil
}

func WalkList(expr Sexp, visit func(Sexp)) {
	if expr == SexpNull {
		return
	}

	var list SexpPair
	switch e := expr.(type) {
	case SexpPair:
		list = e
	default:
		return
	}

	visit(list.head)

	WalkList(list.tail, visit)
}

func ConcatList(a SexpPair, b Sexp) (Sexp, error) {
	if !IsList(b) {
		return SexpNull, fmt.Errorf("ConcatList, second argument wasn't a list it was %T, %v", b, b.SexpString())
	}

	if a.tail == SexpNull {
		return Cons(a.head, b), nil
	}

	switch t := a.tail.(type) {
	case SexpPair:
		newtail, err := ConcatList(t, b)
		if err != nil {
			return SexpNull, err
		}
		return Cons(a.head, newtail), nil
	}

	return SexpNull, fmt.Errorf("ConcatList, first argument pair wasn't a list tail was %T", a.tail)
}
