package glisp

import (
	"fmt"
)

func MapArray(env *Glisp, fun SexpFunction, arr SexpArray) (SexpArray, error) {
	result := make([]Sexp, len(arr))
	var err error

	for i := range arr {
		result[i], err = env.Apply(fun, arr[i:i+1])
		if err != nil {
			return SexpArray(result), err
		}
	}

	return SexpArray(result), nil
}

func FoldrArray(env *Glisp, fun SexpFunction, arr SexpArray, acc Sexp) (Sexp, error) {
	var err error

	for i := len(arr) - 1; i > -1; i-- {
		acc, err = env.Apply(fun, []Sexp{arr[i], acc})
		if err != nil {
			return acc, err
		}
	}

	return acc, nil
}

func FoldlArray(env *Glisp, fun SexpFunction, arr SexpArray, acc Sexp) (Sexp, error) {
	var err error

	for i := range arr {
		acc, err = env.Apply(fun, []Sexp{arr[i], acc})
		if err != nil {
			return acc, err
		}
	}

	return acc, nil
}

func StringArrayToArray(list []string) SexpArray {
	ret := make([]Sexp, 0, len(list))
	for _, v := range list {
		ret = append(ret, SexpStr(v))
	}
	return ret
}

func ConcatArray(arr SexpArray, expr Sexp) (SexpArray, error) {
	var arr2 SexpArray
	switch t := expr.(type) {
	case SexpArray:
		arr2 = t
	case SexpSentinel:
		return arr, nil
	default:
		return arr, fmt.Errorf("concat, second argument is not an array; got %T", expr)
	}

	return append(arr, arr2...), nil
}
