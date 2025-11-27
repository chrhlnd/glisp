package glispext

import (
	"github.com/chrhlnd/glisp"
	"os/exec"
	"os"
	"fmt"
//	"log"
)

func execFunction(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) < 1 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	var ok bool
	var cmdAry glisp.SexpArray

	if cmdAry, ok = args[0].(glisp.SexpArray); !ok {
		return glisp.SexpNull, fmt.Errorf("First param must be an array, index 0 is the command the rest are params")
	}

	cmds := make([]string, len(cmdAry))

	for i, v := range cmdAry {
		if sv, ok := v.(glisp.SexpStr); !ok {
			return glisp.SexpNull, fmt.Errorf("Cmd array must contain strings; index %v had %T", i, v)
		} else {
			cmds[i] = string(sv)
		}
	}

	cmd := exec.Command(cmds[0], cmds[1:]...)

	cmd.Env = os.Environ()

	info, _ := glisp.MakeHash(nil, "ExecResult")

	for _, line := range args[1:] {
		if sline, ok := line.(glisp.SexpStr); ok {
			cmd.Env = append(cmd.Env, string(sline))
		}
	}

	envArr := make([]glisp.Sexp, len(cmd.Env))
	for i, v := range cmd.Env {
		envArr[i] = glisp.SexpStr(v)
	}

	info.HashSet(glisp.SexpStr("env"), glisp.SexpArray(envArr))

	if out, err := cmd.CombinedOutput(); err != nil {
		info.HashSet(glisp.SexpStr("errorstr"), glisp.SexpStr(err.Error()))
		if ee, ok := err.(*exec.ExitError); ok {
			info.HashSet(glisp.SexpStr("exitcode"), glisp.SexpInt(ee.ExitCode()))
			info.HashSet(glisp.SexpStr("systime"), glisp.SexpInt(ee.SystemTime().Milliseconds()))
			info.HashSet(glisp.SexpStr("usrtime"), glisp.SexpInt(ee.UserTime().Milliseconds()))
			info.HashSet(glisp.SexpStr("output"), glisp.SexpData(out))
		} else {
			info.HashSet(glisp.SexpStr("exitcode"), glisp.SexpInt(1))
			info.HashSet(glisp.SexpStr("output"), glisp.SexpData(out))
		}
	} else {
		info.HashSet(glisp.SexpStr("exitcode"), glisp.SexpInt(0))
		info.HashSet(glisp.SexpStr("output"), glisp.SexpData(out))
	}

	return info, nil
}

func lookPathFunction(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) < 1 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	if _, ok := args[0].(glisp.SexpStr); !ok {
		return glisp.SexpNull, fmt.Errorf("Expected string arg")
	}

	abspath, err := exec.LookPath(string(args[0].(glisp.SexpStr)))
	if err != nil {
		return glisp.SexpNull, nil
	}

	return glisp.SexpStr(abspath), nil
}

func getEnvFunction(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) != 1 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	if _, ok := args[0].(glisp.SexpStr); !ok {
		return glisp.SexpNull, fmt.Errorf("Expected string arg")
	}

	val, exists := os.LookupEnv(string(args[0].(glisp.SexpStr)))

	ret := make([]glisp.Sexp, 2)

	ret[0] = glisp.SexpStr(val)
	ret[1] = glisp.SexpBool(exists)

	return glisp.SexpArray(ret), nil
}

func getEnvironFunction(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) != 0 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	envAry := os.Environ()

	envArr := make([]glisp.Sexp, len(envAry))
	for i, v := range envAry {
		envArr[i] = glisp.SexpStr(v)
	}

	return glisp.SexpArray(envArr), nil
}

func ImportOs(env *glisp.Glisp) {
	env.AddFunction("os-exec", execFunction)
	env.AddFunction("os-lookpath", lookPathFunction)
	env.AddFunction("os-getenv", getEnvFunction)
	env.AddFunction("os-environ", getEnvironFunction)
}

