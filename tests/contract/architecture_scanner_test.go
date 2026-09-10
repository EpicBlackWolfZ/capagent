package contract_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Low-level operations apply to syscall and x/sys/unix alike. Constants and
// data types are intentionally not denied. New wrappers must extend this list
// and its fixtures as part of the approved platform boundary (including #65).
var syscallHostOperations = strings.Fields(`
Open Openat Openat2 Creat Close Read Write Pread Pwrite Readv Writev Preadv Pwritev
Stat Lstat Fstat Statat Fstatat Statx Statfs Fstatfs Statfs64 Fstatfs64 Access Faccessat Faccessat2
Readlink Readlinkat ReadDirent Getdents Getdents64 Seek Dup Dup2 Dup3 FcntlInt FcntlFlock
Mkdir Mkdirat Rmdir Unlink Unlinkat Rename Renameat Renameat2 Link Linkat Symlink Symlinkat
Chmod Fchmod Fchmodat Chown Fchown Lchown Fchownat Truncate Ftruncate
Utime Utimes Futimes UtimesNano UtimesNanoAt Mknod Mknodat Mkfifo Mkfifoat
Getxattr Lgetxattr Fgetxattr Listxattr Llistxattr Flistxattr Setxattr Lsetxattr Fsetxattr
Removexattr Lremovexattr Fremovexattr
Exec ForkExec StartProcess Wait4 Waitid Kill Tgkill PtraceAttach PtraceDetach
Getpid Getppid Gettid Getuid Geteuid Getgid Getegid Getgroups Getresuid Getresgid
Setuid Seteuid Setgid Setegid Setgroups Setreuid Setregid Setresuid Setresgid
Setfsuid Setfsgid Getpgid Getpgrp Getsid Setpgid Setsid Uname
Getcwd Chdir Fchdir Chroot Umask Getrlimit Setrlimit Prlimit Prctl Capget Capset
Getenv Environ Setenv Unsetenv Clearenv
Socket Socketpair Bind Connect Listen Accept Accept4 Shutdown
Sendto Recvfrom Sendmsg Recvmsg GetsockoptInt SetsockoptInt Getsockname Getpeername
Pipe Pipe2 Eventfd EpollCreate EpollCreate1 EpollCtl EpollWait Poll Select
Mmap Munmap Mprotect Mount Unmount PivotRoot Unshare Setns IoctlSetInt IoctlGetInt
Syscall Syscall6 RawSyscall RawSyscall6 Syscall9 RawSyscallNoError
`)

var additionalHostOperations = strings.Fields(`
os.NewFile os.OpenRoot os.OpenInRoot os.CopyFS os.CreateTemp os.MkdirTemp
os.Chown os.Lchown os.Chtimes os.Chdir os.Getwd os.Executable os.Hostname
os.Getpid os.Getppid os.Getuid os.Geteuid os.Getgid os.Getegid os.Getgroups
os.FindProcess os.StartProcess os.Pipe os.DirFS os.UserHomeDir os.UserCacheDir os.UserConfigDir os.TempDir
os.Setenv os.Unsetenv os.Clearenv os.ExpandEnv
path/filepath.Abs path/filepath.EvalSymlinks path/filepath.Glob path/filepath.Walk path/filepath.WalkDir
io/ioutil.ReadFile io/ioutil.WriteFile io/ioutil.ReadDir io/ioutil.TempFile io/ioutil.TempDir
os/user.Current os/user.Lookup os/user.LookupId os/user.LookupGroup os/user.LookupGroupId
net.Dial net.DialTimeout net.DialTCP net.DialUDP net.DialUnix net.Listen net.ListenPacket
net.ListenTCP net.ListenUDP net.ListenUnix net.ListenUnixgram
net.Interfaces net.InterfaceAddrs net.InterfaceByIndex net.InterfaceByName
net.LookupAddr net.LookupCNAME net.LookupHost net.LookupIP net.LookupMX net.LookupNS net.LookupPort net.LookupSRV net.LookupTXT
`)

func monitoredHostPackage(pkg string) bool {
	switch pkg {
	case "os", "os/exec", "syscall", pkgUnix, "path/filepath", "io/ioutil", "os/user", "net":
		return true
	default:
		return false
	}
}

func forbiddenHostSelector(pkg, name string, isTest bool) bool {
	selector := pkg + "." + name
	if (pkg == "syscall" || pkg == pkgUnix) && stringSliceContains(syscallHostOperations, name) {
		// The existing test-only ambient inspection exemption applies to both
		// standard environment entry points, not process-global mutations.
		return !isTest || (name != "Getenv" && name != "Environ")
	}
	return stringSliceContains(hostPrimitiveDenylist, selector) ||
		stringSliceContains(additionalHostOperations, selector) ||
		(!isTest && stringSliceContains(hostAmbientEnvDenylist, selector))
}

// scanHostIOFile is the single classifier for real files and synthetic fixtures.
// A logical repository path determines policy; source bytes never alter exemptions.
func scanHostIOFile(relPath string, source []byte) ([]string, error) {
	relPath = filepath.ToSlash(relPath)
	if isExcludedHostIOPath(relPath) || isHostIOAllowedPath(relPath) {
		return nil, nil
	}
	file, err := parser.ParseFile(token.NewFileSet(), relPath, source, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", relPath, err)
	}
	var violations []string
	aliases := make(map[string]string)
	for _, imp := range file.Imports {
		pkg, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("import in %s: %w", relPath, err)
		}
		alias := filepath.Base(pkg)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		if alias == "." && monitoredHostPackage(pkg) {
			violations = append(violations, fmt.Sprintf("%s: prohibited dot import %s", relPath, pkg))
		}
		if stringSliceContains(hostExecDenylist, pkg) {
			violations = append(violations, fmt.Sprintf("%s: import %s", relPath, pkg))
		}
		aliases[alias] = pkg
	}
	isTest := strings.HasSuffix(relPath, "_test.go")
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		// The parser resolves local declarations. Imported package identifiers
		// have nil Obj, whereas shadowing variables/parameters have an object.
		if !ok || ident.Obj != nil {
			return true
		}
		pkg := aliases[ident.Name]
		if forbiddenHostSelector(pkg, sel.Sel.Name, isTest) {
			violations = append(violations, fmt.Sprintf("%s: %s.%s", relPath, pkg, sel.Sel.Name))
		}
		return true
	})
	sort.Strings(violations)
	return violations, nil
}

func scanHostIODirectory(rootDir string, fixtures bool) ([]string, error) {
	var violations []string
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path != rootDir && (strings.HasPrefix(info.Name(), ".") || info.Name() == "vendor" || info.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		if fixtures {
			rel = "internal/fixture/" + filepath.ToSlash(rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, err := scanHostIOFile(rel, data)
		violations = append(violations, found...)
		return err
	})
	sort.Strings(violations)
	return violations, err
}
