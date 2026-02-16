package glispext

import (
	"github.com/chrhlnd/glisp"
//	"github.com/mitchellh/go-ps"
	"os/exec"
	"os"
	"fmt"
	"log"
	"sync"
	"bytes"
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

type sSpawn struct {
	cmd *exec.Cmd
	done chan struct{}
	closed bool
}

var s_spawns map[int]sSpawn = make(map[int]sSpawn)

func SpawnWaitAll() []error {
	var errs []error

	for _, v := range s_spawns {
		err := v.cmd.Wait()
		if err != nil {
			errs = append(errs, err)
		}
	}

	s_spawns = make(map[int]sSpawn)

	return errs
}

func SpawnKillAll() []error {
	var errs []error

	for _, v := range s_spawns {
		if v.cmd.Process != nil {
			err := v.cmd.Process.Kill()
			if err != nil {
				errs = append(errs, err)
			}
		}
	}

	s_spawns = make(map[int]sSpawn)

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

	if v.cmd.Process == nil {
		return glisp.SexpNull, nil
	}

	if v.cmd.ProcessState == nil || !v.cmd.ProcessState.Exited() {
		v.cmd.Process.Kill()
	}

	v.cmd.Wait()

	delete(s_spawns, spawnId)

	return glisp.SexpInt(v.cmd.ProcessState.ExitCode()), nil
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
	select {
	case <-v.done:
		v.closed = true
	default:
	}

	if !v.closed {
		env.RegisterWaitFn(func()(glisp.Sexp, bool) {
			select {
			case <-v.done:
				v.closed = true
				delete(s_spawns, spawnId)
				return glisp.SexpInt(v.cmd.ProcessState.ExitCode()), false
			default:
			}
			return glisp.SexpNull, true
		})
	}

	if v.closed {
		delete(s_spawns, spawnId)
		return glisp.SexpInt(v.cmd.ProcessState.ExitCode()), nil
	}

	return glisp.SexpNull, nil
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

	select {
	case <-v.done:
		v.closed = true
	default:
	}

	return glisp.SexpBool(!v.closed), nil
}

var s_watchers *ReadWatcherCollection = NewReadWatcherCollection()


type BufferRunner struct {
	buf *bytes.Buffer
	lock *sync.Mutex
	deliver func(int, []byte)
	runner func()
}

func newBufRunner(deliver func(int, []byte)) *BufferRunner {
	return &BufferRunner{&bytes.Buffer{},
		&sync.Mutex{},
		deliver,
		nil}
}

func (b *BufferRunner) AddData(env *glisp.Glisp, id int, data []byte) {
	b.lock.Lock()
	b.buf.Write(data)
	if b.runner == nil {
		b.runner = func() {
			b.lock.Lock()
			batch := b.buf.Bytes()
			b.buf.Reset()
			b.runner = nil
			b.lock.Unlock()
			b.deliver(id, batch)
		}
		env.QueueRun(b.runner)
	}
	b.lock.Unlock()
}

func execSpawnOnStdOut(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) != 2 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	spawnId := int(args[0].(glisp.SexpInt))

	v, ok := s_spawns[spawnId]
	if !ok {
		return glisp.SexpNull, fmt.Errorf("%v, spawn %v doesn't exist", name, spawnId)
	}

	fn := args[1].(glisp.SexpFunction)
	in, err := v.cmd.StdoutPipe()
	if err != nil {
		return glisp.SexpNull, err
	}

	watcherId := spawnId * 2

	runner := newBufRunner(func (id int, batch []byte) {
		res, err := env.Apply(fn, []glisp.Sexp{glisp.SexpData(batch)})
		if err != nil {
			log.Fatal(err)
		}

		if val, ok := res.(glisp.SexpBool); (ok && bool(val)) || len(batch) == 0 {
			s_watchers.RemWatcher(watcherId, id)
		}
	})

	s_watchers.AddWatcher(watcherId, in, func (fnId int, data []byte) {
		runner.AddData(env, fnId, data)
	})

	return args[0], err
}

func execSpawnOnStdErr(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) != 2 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	spawnId := int(args[0].(glisp.SexpInt))

	v, ok := s_spawns[spawnId]
	if !ok {
		return glisp.SexpNull, nil
	}

	fn := args[1].(glisp.SexpFunction)

	in, err := v.cmd.StderrPipe()
	if err != nil {
		return glisp.SexpNull, err
	}

	watcherId := spawnId * 2 + 1

	runner := newBufRunner(func (id int, batch []byte) {
		res, err := env.Apply(fn, []glisp.Sexp{glisp.SexpData(batch)})
		if err != nil {
			log.Fatal(err)
		}

		if val, ok := res.(glisp.SexpBool); (ok && bool(val)) || len(batch) == 0 {
			s_watchers.RemWatcher(watcherId, id)
		}
	})

	s_watchers.AddWatcher(watcherId, in, func (fnId int, data []byte) {
		runner.AddData(env, fnId, data)
	})

	return args[0], err
}

func execSpawnStart(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) != 1 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	id := int(args[0].(glisp.SexpInt))

	v, ok := s_spawns[id]
	if !ok {
		return glisp.SexpNull, fmt.Errorf("%v, invalid spawn id %v", name, id)
	}

	go func() {
		err := v.cmd.Start()
		if err != nil {
			log.Print("spawn start error: ", err)
		}
		v.cmd.Wait()
		close(v.done)
	}()

	return args[0], nil
}

func execSpawnGetEnv(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) != 1 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	id := int(args[0].(glisp.SexpInt))

	v, ok := s_spawns[id]
	if !ok {
		return glisp.SexpNull, fmt.Errorf("%v, invalid spawn id %v", name, id)
	}

	envArr := make([]glisp.Sexp, len(v.cmd.Env))
	for i, v := range v.cmd.Env {
		envArr[i] = glisp.SexpStr(v)
	}

	return glisp.SexpArray(envArr), nil
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

	s_spawn_counter++
	id := s_spawn_counter
	s_spawns[id] = sSpawn{ cmd, make(chan struct{}), false }

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
	env.AddFunction("os-spawn-get-env", execSpawnGetEnv)
	env.AddFunction("os-spawn-start", execSpawnStart)
	env.AddFunction("os-spawn-kill", execSpawnKill)
	env.AddFunction("os-spawn-wait", execSpawnWait)
	env.AddFunction("os-spawn-isalive", execSpawnIsAlive)
	env.AddFunction("os-spawn-on-stdout", execSpawnOnStdOut)
	env.AddFunction("os-spawn-on-stderr", execSpawnOnStdErr)
	env.AddFunction("os-lookpath", lookPathFunction)
	env.AddFunction("os-getenv", getEnvFunction)
	env.AddFunction("os-environ", getEnvironFunction)
}

