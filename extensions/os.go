package glispext

import (
	"github.com/chrhlnd/glisp"
	"github.com/mitchellh/go-ps"
	"os/exec"
	"os"
	"fmt"
//	"log"
)

func parseCmdArgs(arg glisp.Sexp) ([]string, error) {
	var cmds []string

	switch lst := arg.(type) {
	case glisp.SexpArray:
		cmds = make([]string, 0, len(lst))
		for i, v := range lst {
			switch vt := v.(type) {
			case glisp.SexpStr:
				cmds = append(cmds, string(vt))
			default:
				return nil, fmt.Errorf("Cmd array must contain strings; index %v had %T", i, v)
			}
		}
	case glisp.SexpPair:
		cmds = make([]string, 0, 5)
		cur := lst
		i := 0
		walkList:
		for {
			switch vt := cur.Head().(type) {
			case glisp.SexpStr:
				cmds = append(cmds, string(vt))
			default:
				return nil, fmt.Errorf("Cmd list must contain strings; index %v had %T", i, cur.Head())
			}

			switch vt := cur.Tail().(type) {
			case glisp.SexpPair:
				cur = vt
			default:
				break walkList
			}
			i++
		}
	default:
		return nil, fmt.Errorf("First param must be an (list|array), index 0 is the command the rest are params, got %T",
								arg)
	}

	return cmds, nil
}

var s_spawn_counter int = 0
var s_spawns map[int]*exec.Cmd = make(map[int]*exec.Cmd)

func SpawnWaitAll() []error {
	var errs []error

	for _, v := range s_spawns {
		err := v.Wait()
		if err != nil {
			errs = append(errs, err)
		}
	}

	s_spawns = make(map[int]*exec.Cmd)

	return errs
}

func SpawnKillAll() []error {
	var errs []error

	for _, v := range s_spawns {
		if v.Process != nil {
			err := v.Process.Kill()
			if err != nil {
				errs = append(errs, err)
			}
		}
	}

	s_spawns = make(map[int]*exec.Cmd)

	return errs
}

func execSpawnKill(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) != 1 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	spawnId := int(args[0].(glisp.SexpInt))

	v, ok := s_spawns[spawnId]
	if !ok {
		return glisp.SexpNull, fmt.Errorf("invalid spawnid %v", spawnId)
	}

	if v.Process == nil {
		return glisp.SexpNull, nil
	}

	if v.ProcessState == nil || !v.ProcessState.Exited() {
		v.Process.Kill()
	}

	v.Wait()

	delete(s_spawns, spawnId)

	return glisp.SexpInt(v.ProcessState.ExitCode()), nil
}

func execSpawnWait(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) != 1 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	spawnId := int(args[0].(glisp.SexpInt))

	v, ok := s_spawns[spawnId]
	if !ok {
		return glisp.SexpNull, fmt.Errorf("invalid spawnid %v", spawnId)
	}

	err := v.Wait()

	delete(s_spawns, spawnId)

	return glisp.SexpNull, err
}

func execSpawnIsAlive(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) != 1 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	spawnId := int(args[0].(glisp.SexpInt))

	v, ok := s_spawns[spawnId]
	if !ok {
		return glisp.SexpBool(ok), nil
	}

	p, err := ps.FindProcess(v.Process.Pid)

	ret := p != nil && err == nil

	return glisp.SexpBool(ret), nil
}

func execSpawn(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) < 1 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	cmds, err := parseCmdArgs(args[0])
	if err != nil {
		return glisp.SexpNull, err
	}

	cmd := exec.Command(cmds[0], cmds[1:]...)

	cmd.Env = os.Environ()

	for _, line := range args[1:] {
		if sline, ok := line.(glisp.SexpStr); ok {
			cmd.Env = append(cmd.Env, string(sline))
		}
	}

	envArr := make([]glisp.Sexp, len(cmd.Env))
	for i, v := range cmd.Env {
		envArr[i] = glisp.SexpStr(v)
	}

	err = cmd.Start()
	if err != nil {
		return glisp.SexpNull, err
	}

	s_spawn_counter++
	id := s_spawn_counter
	s_spawns[id] = cmd

	return glisp.SexpInt(id), nil
}

func execFunction(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) < 1 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	cmds, err := parseCmdArgs(args[0])
	if err != nil {
		return glisp.SexpNull, err
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
	env.AddFunction("os-spawn", execSpawn)
	env.AddFunction("os-spawn-kill", execSpawnKill)
	env.AddFunction("os-spawn-wait", execSpawnWait)
	env.AddFunction("os-spawn-isalive", execSpawnIsAlive)
	env.AddFunction("os-lookpath", lookPathFunction)
	env.AddFunction("os-getenv", getEnvFunction)
	env.AddFunction("os-environ", getEnvironFunction)
}

