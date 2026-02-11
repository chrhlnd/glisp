package glispext

import (
	"errors"
	"fmt"
	"github.com/chrhlnd/glisp"
	"time"
)

type SexpTime time.Time

func (t SexpTime) SexpString() string {
	return time.Time(t).String()
}

func TimeFunction(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	return SexpTime(time.Now()), nil
}

func TimeFormatFunction(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) < 1 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	t := time.Time(args[0].(SexpTime))

	fmt := time.DateTime

	if len(args) > 1 {
		fmt = string(args[1].(glisp.SexpStr))
	}

	return glisp.SexpStr(t.Format(fmt)), nil
}

func TimeitFunction(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) != 1 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	var fun glisp.SexpFunction
	switch t := args[0].(type) {
	case glisp.SexpFunction:
		fun = t
	default:
		return glisp.SexpNull,
			errors.New("argument of timeit should be function")
	}

	starttime := time.Now()
	elapsed := time.Since(starttime)
	maxseconds := 10.0
	var iterations int

	for iterations = 0; iterations < 10000; iterations++ {
		_, err := env.Apply(fun, []glisp.Sexp{})
		if err != nil {
			return glisp.SexpNull, err
		}
		elapsed = time.Since(starttime)
		if elapsed.Seconds() > maxseconds {
			break
		}
	}

	fmt.Printf("ran %d iterations in %f seconds\n",
		iterations, elapsed.Seconds())
	fmt.Printf("average %f seconds per run\n",
		elapsed.Seconds()/float64(iterations))

	return glisp.SexpNull, nil
}

func ImportTime(env *glisp.Glisp) {
	env.AddFunction("time", TimeFunction)
	env.AddFunction("time-format", TimeFormatFunction)
	env.AddFunction("timeit", TimeitFunction)
}
