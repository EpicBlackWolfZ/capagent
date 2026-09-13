"""Fail-closed policies for the bounded delegated-context syscall inventory."""
import json
import re

# %file/%network/%process/%creds cover path and identity variants. Explicit
# descriptor calls cover operations those classes omit, without tracing file
# contents through read/pread. -yy identifies pipe writes and file-backed maps.
TRACE_CALLS = (
    '%process,%file,%network,%creds,%memory,prctl,capset,setns,unshare,memfd_create,open_by_handle_at,'
    'fchmod,fchown,fchown32,ftruncate,ftruncate64,fallocate,fsync,fdatasync,sync,syncfs,sync_file_range,'
    'fsetxattr,fremovexattr,ioctl,fcntl,fcntl64,flock,eventfd,eventfd2,write,writev,pwrite64,pwritev,pwritev2,'
    'sendfile,sendfile64,copy_file_range,splice,tee,vmsplice,io_uring_setup,io_uring_enter,io_uring_register'
)
TRACE_OPTIONS = ['-f', '-q', '-I', '2', '-s', '320', '-yy', '-e', 'trace=' + TRACE_CALLS]
THREAD_FLAGS = {'CLONE_VM', 'CLONE_FS', 'CLONE_FILES', 'CLONE_SIGHAND', 'CLONE_THREAD', 'CLONE_SYSVSEM',
                'CLONE_SETTLS', 'CLONE_PARENT_SETTID', 'CLONE_CHILD_SETTID', 'CLONE_CHILD_CLEARTID'}
PROCESS_FLAGS = {'CLONE_VM', 'CLONE_VFORK', 'CLONE_PIDFD', 'SIGCHLD'}
MUTATIONS = re.compile(
    r'(?:mkdir.*|rmdir|unlink.*|rename.*|link.*|symlink.*|.*chmod.*|.*chown.*|.*truncate.*|'
    r'.*utime.*|mknod.*|mount.*|.*umount.*|.*setxattr.*|.*removexattr.*|fallocate|'
    r'fsync|fdatasync|sync.*|setns|unshare|pivot_root|chroot|open_tree|move_mount|fsopen|fsconfig|fsmount|'
    r'open_by_handle_at|memfd_create|shmget|shmat|shmdt|shmctl|capset|setuid.*|setgid.*|setreuid.*|setregid.*|setfsuid.*|setfsgid.*|'
    r'pwrite.*|sendfile.*|copy_file_range|splice|tee|vmsplice|io_uring_.*|creat|quotactl.*)')
NETWORK = {'connect', 'bind', 'listen', 'accept', 'accept4', 'sendto', 'sendmsg', 'sendmmsg',
           'recvfrom', 'recvmsg', 'recvmmsg', 'socket', 'socketpair', 'shutdown', 'setsockopt', 'getsockopt'}
MANAGER_ARGS = ['--user', '--no-pager', '--no-ask-password', 'show', '--property=Version', '--value']


def plain_fds(text):
    """Remove only numeric descriptor annotations, after retaining raw write/map evidence."""
    return re.sub(r'(?<=\d)<(?:[^<>\n]|<[^<>\n]*>)*>', '', text)


def records(text):
    rows = []
    for line in text.splitlines():
        match = re.fullmatch(r'(\d+)\s+(\w+)\((.*)\)\s+= (.*)', line)
        if match:
            rows.append((match[1], match[2], match[3], match[4]))
        elif not re.fullmatch(r'\d+\s+(?:\+\+\+ .* \+\+\+|--- .* ---)', line):
            raise ValueError('unrecognized delegated trace record')
    return rows


def command(args):
    match = re.fullmatch(r'("(?:[^"\\]|\\.)*"), (\[.*?\]), (?:.*)', args)
    if match is None:
        raise ValueError('unreadable executable arguments')
    try:
        executable, argv = json.loads(match[1]), json.loads(match[2])
    except (ValueError, TypeError) as error:
        raise ValueError('truncated or unreviewed executable arguments') from error
    if not isinstance(argv, list) or any(not isinstance(arg, str) for arg in argv):
        raise ValueError('unreviewed executable arguments')
    return executable, argv


def lineage(rows, launcher):
    parents, threads, probes = {}, set(), set()
    for pid, call, args, result in rows:
        if call in {'fork', 'vfork', 'execveat'}:
            raise ValueError('unreviewed process creation or execution')
        if call not in {'clone', 'clone3'}:
            continue
        match = re.search(r'\bflags=([^,}\s)]+)', args)
        if match is None:
            raise ValueError('unreadable clone flags')
        flags = set(match[1].split('|'))
        if not flags or not flags <= (THREAD_FLAGS if 'CLONE_THREAD' in flags else PROCESS_FLAGS):
            raise ValueError('namespace or unreviewed clone flags')
        if result.startswith('-1 '):
            continue
        if not result.isdecimal() or result == launcher or result in parents:
            raise ValueError('invalid child lineage')
        parents[result] = pid
        if 'CLONE_THREAD' in flags:
            threads.add(result)
        elif 'SIGCHLD' not in flags:
            if flags != {'CLONE_VM', 'CLONE_VFORK', 'CLONE_PIDFD'}:
                raise ValueError('unreviewed launcher probe')
            probes.add(result)
    for pid, _, _, _ in rows:
        seen = set()
        current = pid
        while current != launcher:
            if current in seen or current not in parents:
                raise ValueError('missing or cyclic process lineage')
            seen.add(current)
            current = parents[current]
    for pid in probes:
        calls = [(call, args, result) for owner, call, args, result in rows if owner == pid]
        if calls != [('exit_group', '0', '?')]:
            raise ValueError('unexpected PIDFD launcher probe activity')
    return parents, threads, probes


def group(pid, parents, threads):
    while pid in threads:
        pid = parents[pid]
    return pid


def executions(rows, binary, parents, threads, probes, query):
    execs = [(pid, *command(args), result) for pid, call, args, result in rows if call == 'execve']
    if not execs or execs[0][1] != str(binary) or execs[0][3] != '0':
        raise ValueError('missing launching executable')
    if len({row[0] for row in execs}) != len(execs):
        raise ValueError('repeated process execution')
    launcher, _, argv, _ = execs[0]
    allowed_tail = ['--json', '--active'] if query else ['--json']
    if len(argv) != len(allowed_tail) + 2 or argv[0] != str(binary) or argv[2:] != allowed_tail or not re.fullmatch(
            r'--context=(?:uid:[0-9]+|user:[a-zA-Z0-9_.-]+)', argv[1]):
        raise ValueError('unreviewed launcher arguments')
    workers = [row for row in execs if row[1] == '/proc/self/fd/3']
    if len(workers) != 1 or workers[0][2:] != (['/proc/self/fd/3', '--internal-target-worker'], '0'):
        raise ValueError('worker did not execute its exact pinned invocation')
    worker = workers[0][0]
    if worker not in parents or worker in threads or group(parents[worker], parents, threads) != launcher:
        raise ValueError('worker was not a direct launcher child')
    metadata = []
    for pid, executable, args, _ in execs[1:]:
        if pid == worker:
            continue
        if executable not in {'/usr/bin/systemctl', '/bin/systemctl'} or group(parents[pid], parents, threads) != worker:
            raise ValueError('unexpected worker child executable')
        expected = MANAGER_ARGS if query and pid == query[0] else ['--version']
        if args != [executable, *expected]:
            raise ValueError('unreviewed systemctl arguments')
        metadata.append(pid)
    if len(metadata) > (2 if query else 1) or len(set(metadata)) != len(metadata):
        raise ValueError('repeated metadata command')
    expected_children = {worker, *metadata, *probes}
    if set(parents) - threads != expected_children:
        raise ValueError('unreviewed non-thread child')
    if len(probes) > 2 or any(group(parents[pid], parents, threads) not in {launcher, worker} for pid in probes):
        raise ValueError('unexpected PIDFD probe lineage')
    if target_uid := re.fullmatch(r'--context=uid:(\d+)', argv[1]):
        selected_uid = int(target_uid[1])
    else:
        selected_uid = None
    return launcher, worker, selected_uid


def credentials(rows, worker, parents, threads, target):
    operations = {}
    worker_executed = False
    for pid, call, args, result in rows:
        if call == 'execve' and pid == worker:
            worker_executed = True
        if call not in {'setgroups', 'setgroups32', 'setresgid', 'setresgid32', 'setresuid', 'setresuid32'}:
            continue
        if not worker_executed or group(pid, parents, threads) != worker or result != '0':
            raise ValueError('credential change outside the pinned worker')
        prior = operations.setdefault(pid, [])
        expected = ('setgroups', 'setresgid', 'setresuid')
        kind = call.removesuffix('32')
        if len(prior) == len(expected) or kind != expected[len(prior)]:
            raise ValueError('unreviewed credential transition order')
        if kind == 'setgroups':
            match = re.fullmatch(r'(\d+), (\[[0-9, ]*\])', args)
            if match is None:
                raise ValueError('unreadable supplementary groups')
            groups = json.loads(match[2])
            if len(groups) != int(match[1]) or target and groups != target['groups']:
                raise ValueError('unexpected supplementary groups')
        else:
            ids = args.split(', ')
            if len(ids) != 3 or not ids[0].isdecimal() or len(set(ids)) != 1:
                raise ValueError('incomplete credential drop')
            if target and int(ids[0]) != target['gid' if kind == 'setresgid' else 'uid']:
                raise ValueError('credentials differ from selected target')
        prior.append((kind, args))
    if worker not in operations or len(operations[worker]) != 3:
        raise ValueError('missing worker credential transitions')
    if any(value != operations[worker] for value in operations.values()):
        raise ValueError('worker threads disagree on credentials')


def operations(rows, raw_rows, query, owners=None):
    eventfds, legacy_eventfds = set(), set()
    owners = owners or {}
    for (pid, call, args, result), (_, _, raw, raw_result) in zip(rows, raw_rows):
        owner = owners.get(pid, pid)
        if call == 'eventfd2' and args == '0, EFD_CLOEXEC|EFD_NONBLOCK':
            match = re.search(r'eventfd-id=(\d+)', raw_result)
            if match:
                eventfds.add((owner, match[1]))
            legacy = re.fullmatch(r'(\d+)<anon_inode:\[eventfd\]>', raw_result)
            if legacy:
                legacy_eventfds.add((owner, legacy[1]))
        if MUTATIONS.fullmatch(call) or call in {'open', 'openat', 'openat2'} and re.search(
                r'\bO_(?:WRONLY|RDWR|CREAT|TRUNC|APPEND|TMPFILE)\b', args):
            raise ValueError('passive delegation attempted a mutation')
        if call in {'open', 'openat', 'openat2'} and 'O_RDONLY' not in args and 'O_PATH' not in args:
            raise ValueError('unreadable open flags')
        if call in NETWORK:
            if not query or pid != query[0]:
                raise ValueError('network operation outside the reviewed manager query')
            if call == 'socket':
                if args != 'AF_UNIX, SOCK_STREAM|SOCK_CLOEXEC|SOCK_NONBLOCK, 0':
                    raise ValueError('unreviewed manager socket')
            elif call not in {'connect', 'bind', 'sendmsg', 'recvmsg', 'getsockopt', 'setsockopt'} or not args.startswith(query[1] + ','):
                raise ValueError('unreviewed manager network operation')
            elif call in {'sendmsg', 'recvmsg'} and 'msg_name=NULL' not in args:
                if call != 'recvmsg' or not result.startswith('-1 EAGAIN ') or 'msg_namelen=0' not in args:
                    raise ValueError('manager message contains another destination')
            elif call == 'setsockopt' and not re.fullmatch(
                    r'\d+, SOL_SOCKET, SO_(?:RCVBUF|SNDBUF|RCVBUFFORCE|SNDBUFFORCE), \[8388608\], 4', args):
                raise ValueError('unreviewed manager socket option')
        if call in {'write', 'writev'}:
            pipe = re.match(r'\d+<pipe:\[\d+\]>,', raw)
            query_write = query and pid == query[0] and args.startswith(query[1] + ',')
            event = re.match(r'\d+<\{eventfd-count=\d+, eventfd-id=(\d+), eventfd-semaphore=0\}>,', raw)
            legacy = re.match(r'(\d+)<anon_inode:\[eventfd\]>,', raw)
            known_event = event and (owner, event[1]) in eventfds or legacy and (owner, legacy[1]) in legacy_eventfds
            wakeup = known_event and args.split(', ', 1)[-1] == r'"\1\0\0\0\0\0\0\0", 8'
            if pipe is None and not query_write and not wakeup:
                raise ValueError('write outside report/worker pipes or reviewed query socket')
        if call in {'mmap', 'mmap2'} and 'PROT_WRITE' in args and 'MAP_SHARED' in args and 'MAP_ANONYMOUS' not in args:
            raise ValueError('writable shared file mapping')
        if call == 'flock' or call in {'fcntl', 'fcntl64'} and not re.search(
                r', (?:F_GETFD|F_GETFL|F_SETFD|F_SETFL|F_DUPFD_CLOEXEC)(?:,|$)', args):
            raise ValueError('unreviewed descriptor control operation')
        if call in {'fcntl', 'fcntl64'} and ', F_SETFL,' in args and not re.fullmatch(
                r'\d+, F_SETFL, (?:O_RDONLY|O_WRONLY)(?:\|O_NONBLOCK)?(?:\|O_LARGEFILE)?', args):
            raise ValueError('unreviewed file status flags')
        if call in {'fcntl', 'fcntl64'} and ', F_SETFD,' in args and not re.fullmatch(r'\d+, F_SETFD, (?:0|FD_CLOEXEC)', args):
            raise ValueError('unreviewed descriptor flags')
        if call == 'ioctl' and not re.search(r', (?:TCGETS|TCGETS2|TIOCGWINSZ|FIONREAD)(?:,|$)', args):
            raise ValueError('unreviewed descriptor ioctl')
        if call == 'prctl' and not (args.startswith(('PR_GET_NO_NEW_PRIVS,', 'PR_CAPBSET_READ,', 'PR_GET_DUMPABLE,', 'PR_GET_NAME,')) or
                                   args.startswith('PR_SET_VMA, PR_SET_VMA_ANON_NAME,')):
            raise ValueError('unreviewed process authority operation')


def verify(text, binary, target=None, query=None):
    if len(text) > 16 * 1024 * 1024:
        raise ValueError('delegated trace exceeds the validation budget')
    raw_rows = records(text)
    rows = records(plain_fds(text))
    launch = next((pid for pid, call, _, _ in rows if call == 'execve'), None)
    if launch is None:
        raise ValueError('missing launcher')
    parents, threads, probes = lineage(rows, launch)
    _, worker, selected_uid = executions(rows, binary, parents, threads, probes, query)
    if selected_uid is not None and target and target['uid'] != selected_uid:
        raise ValueError('trace selector differs from target')
    credentials(rows, worker, parents, threads, target)
    if selected_uid is not None and not any(
            pid == worker and call == 'setresuid' and args == ', '.join([str(selected_uid)] * 3)
            for pid, call, args, _ in rows):
        raise ValueError('worker credentials differ from numeric selector')
    owners = {pid: group(pid, parents, threads) for pid, _, _, _ in rows}
    operations(rows, raw_rows, query, owners)
    verify_signals(rows, parents, threads, worker)
    verify_complete(text, rows, parents, threads, launch)


def verify_signals(rows, parents, threads, worker):
    for pid, call, args, _ in rows:
        if call not in {'kill', 'tkill', 'tgkill', 'pidfd_send_signal'}:
            continue
        values = args.split(', ')
        if call in {'kill', 'pidfd_send_signal'} and len(values) > 1 and values[1] == '0':
            continue
        pids = {row[0] for row in rows}
        if call != 'tgkill' or len(values) != 3 or values[2] not in {'SIGURG', 'SIGRT_1'} or values[1] not in pids:
            raise ValueError('unreviewed process signal')
        owner = group(pid, parents, threads)
        if values[2] == 'SIGRT_1' and owner != worker:
            raise ValueError('credential signal outside the worker')
        if values[0] != owner or group(values[1], parents, threads) != owner:
            raise ValueError('signal outside the current thread group')


def verify_complete(text, rows, parents, threads, launcher):
    terminal = dict(re.findall(r'^(\d+)\s+\+\+\+ exited with (\d+) \+\+\+$', text, re.M))
    pids = {launcher, *parents}
    if set(terminal) != pids:
        raise ValueError('missing or unexpected terminal process records')
    exits = {(group(pid, parents, threads), args) for pid, call, args, _ in rows if call == 'exit_group'}
    if any((group(pid, parents, threads), terminal[pid]) not in exits for pid in pids):
        raise ValueError('terminal process status lacks matching group exit')
