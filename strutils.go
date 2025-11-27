package glisp

import (
	"errors"
)

func ConcatStr(str SexpStr, expr Sexp) (SexpStr, error) {
	var str2 SexpStr
	switch t := expr.(type) {
	case SexpStr:
		str2 = t
	case SexpSentinel:
		str2 = ""
	default:
		return SexpStr(""), errors.New("second argument is not a string")
	}

	return str + str2, nil
}

func AppendStr(str SexpStr, expr Sexp) (SexpStr, error) {
	var chr SexpChar
	switch t := expr.(type) {
	case SexpChar:
		chr = t
	default:
		return SexpStr(""), errors.New("second argument is not a char")
	}

	return str + SexpStr(chr), nil
}


func FoldrString(env *Glisp, fun SexpFunction, arr SexpStr, acc Sexp) (Sexp, error) {
	var err error

	for i := len(arr) - 1; i > -1; i-- {
		acc, err = env.Apply(fun, []Sexp{SexpChar(arr[i]), acc})
		if err != nil {
			return acc, err
		}
	}

	return acc, nil
}

func FoldlString(env *Glisp, fun SexpFunction, arr SexpStr, acc Sexp) (Sexp, error) {
	var err error

	for i := range string(arr) {
		acc, err = env.Apply(fun, []Sexp{SexpChar(arr[i]), acc})
		if err != nil {
			return acc, err
		}
	}

	return acc, nil
}

