package glispext

import (
	"github.com/chrhlnd/glisp"
	"strings"
	"os/exec"
	"os"
	"fmt"
	"log"
	"sync"
	"bytes"
	"sync/atomic"
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
	watcherOut chan struct{}
	watcherErr chan struct{}
}

var s_spawns map[int]*sSpawn = make(map[int]*sSpawn)

func SpawnWaitAll() []error {
	var errs []error

	for _, v := range s_spawns {
		err := v.cmd.Wait()
		if err != nil {
			errs = append(errs, err)
		}
	}

	s_spawns = make(map[int]*sSpawn)

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

	s_spawns = make(map[int]*sSpawn)

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

	waitfor := make(chan struct{})

	var ret glisp.Sexp

	go func() {
		<-v.done
		delete(s_spawns, spawnId)
		ret = glisp.SexpInt(v.cmd.ProcessState.ExitCode())
		if v.watcherOut != nil {
			//log.Print("Waiting for out")
			WAIT:
			for v.watcherOut != nil {
				select {
				case <-v.watcherOut:
					break WAIT;
				default:
					if env.QueuedDraining() {
						panic("watchout Can't do this from a draining context")
					}
					env.CallQueued()
				}
			}
			//log.Print("Done waiting for out")
		}
		if v.watcherErr != nil {
			//log.Print("Waiting for err")
			WAIT1:
			for v.watcherErr != nil {
				select {
				case <-v.watcherErr:
					break WAIT1;
				default:
					if env.QueuedDraining() {
						panic("watcherr Can't do this from a draining context")
					}
					env.CallQueued()
				}
			}
			//log.Print("Done waiting for err")
		}
		close(waitfor)
	}()

	<-waitfor

	return ret, nil
}

func execSpawnIsAlive(env *glisp.Glisp, name string, args []glisp.Sexp) (glisp.Sexp, error) {
	if len(args) != 1 {
		return glisp.SexpNull, glisp.WrongNargs
	}

	spawnId := int(args[0].(glisp.SexpInt))

	_, ok := s_spawns[spawnId]
	if !ok {
		return glisp.SexpBool(false), nil
	}

	return glisp.SexpBool(true), nil
}

var s_watchers *ReadWatcherCollection = NewReadWatcherCollection()

type BufferRunner struct {
	buf *bytes.Buffer
	lock *sync.Mutex
	deliver func(int, []byte)
	runner func()
	closed atomic.Bool
	tag string
}

func newBufRunner(tag string, deliver func(int, []byte)) *BufferRunner {
	return &BufferRunner{&bytes.Buffer{},
		&sync.Mutex{},
		deliver,
		nil,
		atomic.Bool{},
		tag}
}

func (b *BufferRunner) AddData(env *glisp.Glisp, id int, data []byte) {
	b.lock.Lock()
	//log.Print("Collecting data size ", len(data), " ", b.tag)
	b.buf.Write(data)

	if len(data) == 0 {
		//log.Print("Closing runner ", b.tag)
		b.closed.Store(true)
	}

	if b.runner == nil {
		b.runner = func() {
			//log.Print("Ran runner ", b.tag)
			b.lock.Lock()
			batch := make([]byte, b.buf.Len())
			copy(batch, b.buf.Bytes())
			b.buf.Reset()
			b.runner = nil
			b.lock.Unlock()
			//log.Print("Delivering batch size ", len(batch), " ", b.tag)
			b.deliver(id, batch)
			if b.closed.Load() && len(batch) > 0 {
				//log.Print("Deliverying close ", b.tag)
				b.deliver(id, batch[:0])
			}
		}
		env.QueueRun(b.runner)
		//log.Print("Queued runner for ", b.tag)
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

	if v.watcherOut == nil {
		v.watcherOut = make(chan struct{})
	}

	tag := "stdout: " + v.cmd.Path + " " + strings.Join(v.cmd.Args, ",")

	runner := newBufRunner(tag, func (id int, batch []byte) {
		//log.Print("OnStdOut running being called with batch size ", len(batch))
		//log.Print(" for ", v.cmd.Path, " ", strings.Join(v.cmd.Args, ","))
		if len(batch) == 0 && v.watcherOut != nil {
			close(v.watcherOut)
			v.watcherOut = nil
		}

		res, err := env.Apply(fn, []glisp.Sexp{glisp.SexpData(batch)})
		if err != nil {
			log.Fatal(err)
		}

		if val, ok := res.(glisp.SexpBool); (ok && bool(val)) || len(batch) == 0 {
			if v.watcherOut != nil {
				close(v.watcherOut)
				v.watcherOut = nil
			}
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

	if v.watcherErr == nil {
		v.watcherErr = make(chan struct{})
	}

	tag := "stderr " + v.cmd.Path + " " + strings.Join(v.cmd.Args, ",")

	runner := newBufRunner(tag, func (id int, batch []byte) {
		//log.Print("OnStdError running being called with batch size ", len(batch))
		//log.Print(" for ", v.cmd.Path, " ", strings.Join(v.cmd.Args, ","))

		if len(batch) == 0 && v.watcherErr != nil {
			close(v.watcherErr)
			v.watcherErr = nil
		}

		res, err := env.Apply(fn, []glisp.Sexp{glisp.SexpData(batch)})
		if err != nil {
			log.Fatal(err)
		}

		if val, ok := res.(glisp.SexpBool); (ok && bool(val)) || len(batch) == 0 {
			if v.watcherErr != nil {
				close(v.watcherErr)
				v.watcherErr = nil
			}
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
	s_spawns[id] = &sSpawn{ cmd, make(chan struct{}), nil, nil }

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

