// Goodman eBPF sensor: intercept security-relevant syscalls from watched
// processes and ship each one to user space together with the user-space
// call stack (frame-pointer walk), so the sensor can attribute the syscall
// to the npm/PyPI package that caused it.
//
// CO-RE: compiled once against vmlinux.h, runs on any kernel >= 5.8 with BTF.
#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>
#include "goodman.h"

char LICENSE[] SEC("license") = "GPL";

/* AF constants (not exported via BTF). */
#define AF_INET  2
#define AF_INET6 10

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 24); /* 16 MiB */
} events SEC(".maps");

/* Only trace processes we care about. Populated from user space (tgid -> 1). */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, __u32);
    __type(value, __u8);
    __uint(max_entries, 4096);
} watched_pids SEC(".maps");

/* Dropped-event counter (ring buffer full), index 0. */
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __type(key, __u32);
    __type(value, __u64);
    __uint(max_entries, 1);
} drops SEC(".maps");

/* --- LSM enforcement maps (written only by user space) --- */

struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __type(key, __u32);
    __type(value, __u64);
    __uint(max_entries, 1);
} enforce_deadline SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, __u64);
    __type(value, __u8);
    __uint(max_entries, 4096);
} enforced_cgroups SEC(".maps");

struct deny_path {
    char path[PATH_MAX_LEN];
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, struct deny_path);
    __type(value, __u8);
    __uint(max_entries, 1024);
} deny_open SEC(".maps");

struct deny_addr {
    __u8  family;
    __u8  _pad;
    __u16 port;
    __u8  addr[16];
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, struct deny_addr);
    __type(value, __u8);
    __uint(max_entries, 1024);
} deny_connect SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, struct deny_path);
    __type(value, __u8);
    __uint(max_entries, 1024);
} deny_exec SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __type(key, __u32);
    __type(value, __u64);
    __uint(max_entries, 1);
} deny_event_drops SEC(".maps");

#ifndef EPERM
#define EPERM 1
#endif

static __always_inline void count_deny_drop(void)
{
    __u32 zero = 0;
    __u64 *d = bpf_map_lookup_elem(&deny_event_drops, &zero);
    if (d)
        __sync_fetch_and_add(d, 1);
}

static __always_inline int enforce_active(void)
{
    __u32 zero = 0;
    __u64 *deadline = bpf_map_lookup_elem(&enforce_deadline, &zero);
    if (!deadline || *deadline == 0)
        return 0;
    return bpf_ktime_get_ns() < *deadline;
}

static __always_inline int in_enforced_cgroup(void)
{
    __u64 cg = bpf_get_current_cgroup_id();
    return bpf_map_lookup_elem(&enforced_cgroups, &cg) != NULL;
}

static __always_inline int emit_deny(void *ctx, __u32 tgid, __u8 type, const char *arg, int arg_len)
{
    struct event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) {
        count_deny_drop();
        return 0;
    }
    e->pid = tgid;
    e->tid = (__u32)bpf_get_current_pid_tgid();
    e->type = type;
    e->timestamp_ns = bpf_ktime_get_ns();
    bpf_get_current_comm(&e->comm, sizeof(e->comm));
    __builtin_memset(e->arg, 0, sizeof(e->arg));
    if (arg && arg_len > 0) {
        int n = arg_len;
        if (n >= PATH_MAX_LEN)
            n = PATH_MAX_LEN - 1;
        bpf_probe_read_kernel(e->arg, n, arg);
        e->arg[n] = 0;
    }
    long stk = bpf_get_stack(ctx, e->stack, sizeof(e->stack), BPF_F_USER_STACK);
    e->stack_len = stk > 0 ? (__u32)(stk / sizeof(__u64)) : 0;
    bpf_ringbuf_submit(e, 0);
    return 0;
}

static __always_inline int deny_return(void *ctx, __u32 tgid, __u8 type, const char *arg, int arg_len)
{
    emit_deny(ctx, tgid, type, arg, arg_len);
    return -EPERM;
}

static __always_inline void count_drop(void)
{
    __u32 zero = 0;
    __u64 *d = bpf_map_lookup_elem(&drops, &zero);
    if (d)
        __sync_fetch_and_add(d, 1);
}

static __always_inline struct event *reserve_event(void *ctx, __u32 tgid, __u8 type)
{
    struct event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) {
        count_drop();
        return NULL;
    }
    e->pid = tgid;
    e->tid = (__u32)bpf_get_current_pid_tgid();
    e->type = type;
    e->timestamp_ns = bpf_ktime_get_ns();
    bpf_get_current_comm(&e->comm, sizeof(e->comm));
    e->arg[0] = 0;
    /* THE KEY LINE: capture the user-space stack via frame pointers. */
    long n = bpf_get_stack(ctx, e->stack, sizeof(e->stack), BPF_F_USER_STACK);
    e->stack_len = n > 0 ? (__u32)(n / sizeof(__u64)) : 0;
    return e;
}

static __always_inline int watched(__u32 *tgid)
{
    *tgid = bpf_get_current_pid_tgid() >> 32;
    return bpf_map_lookup_elem(&watched_pids, tgid) != NULL;
}

static __always_inline int submit_file_open(struct trace_event_raw_sys_enter *ctx, __u64 filename_arg)
{
    __u32 tgid;
    if (!watched(&tgid))
        return 0;

    struct event *e = reserve_event(ctx, tgid, EVENT_FILE_OPEN);
    if (!e)
        return 0;
    const char *path = (const char *)filename_arg;
    bpf_probe_read_user_str(&e->arg, sizeof(e->arg), path);
    bpf_ringbuf_submit(e, 0);
    return 0;
}

SEC("tracepoint/syscalls/sys_enter_open")
int trace_open(struct trace_event_raw_sys_enter *ctx)
{
    return submit_file_open(ctx, ctx->args[0]); /* open: filename */
}

SEC("tracepoint/syscalls/sys_enter_openat")
int trace_openat(struct trace_event_raw_sys_enter *ctx)
{
    return submit_file_open(ctx, ctx->args[1]); /* openat: filename */
}

SEC("tracepoint/syscalls/sys_enter_openat2")
int trace_openat2(struct trace_event_raw_sys_enter *ctx)
{
    return submit_file_open(ctx, ctx->args[1]); /* openat2: filename */
}

/* Render "a.b.c.d:port" or "[v6-hex]:port" into e->arg without snprintf
 * (bpf_snprintf needs kernel 5.13; keep the floor at 5.8).
 *
 * Every write masks its index with (PATH_MAX_LEN-1) so the verifier can
 * statically prove the store is in bounds regardless of the data-dependent
 * position. PATH_MAX_LEN is a power of two (256), so the mask is exact. */
#define ARG_MASK (PATH_MAX_LEN - 1)
#define PUT(buf, pos, c) do { (buf)[(pos) & ARG_MASK] = (c); (pos)++; } while (0)

static __always_inline int put_u8_dec(char *buf, int pos, __u8 v)
{
    if (v >= 100)
        PUT(buf, pos, '0' + v / 100);
    if (v >= 10)
        PUT(buf, pos, '0' + (v / 10) % 10);
    PUT(buf, pos, '0' + v % 10);
    return pos;
}

static __always_inline int put_u16_dec(char *buf, int pos, __u16 v)
{
    char tmp[5];
    int n = 0;
    if (v == 0) {
        PUT(buf, pos, '0');
        return pos;
    }
#pragma unroll
    for (int i = 0; i < 5; i++) {
        if (v == 0)
            break;
        tmp[n++] = '0' + v % 10;
        v /= 10;
    }
#pragma unroll
    for (int i = 4; i >= 0; i--) {
        if (i < n)
            PUT(buf, pos, tmp[i]);
    }
    return pos;
}

static const char hexd[] = "0123456789abcdef";

SEC("tracepoint/syscalls/sys_enter_connect")
int trace_connect(struct trace_event_raw_sys_enter *ctx)
{
    __u32 tgid;
    if (!watched(&tgid))
        return 0;

    struct sockaddr *uaddr = (struct sockaddr *)ctx->args[1];
    __u16 family = 0;
    bpf_probe_read_user(&family, sizeof(family), &uaddr->sa_family);
    if (family != AF_INET && family != AF_INET6)
        return 0; /* skip AF_UNIX etc. — file opens already cover local IPC */

    struct event *e = reserve_event(ctx, tgid, EVENT_NET_CONNECT);
    if (!e)
        return 0;

    int pos = 0;
    if (family == AF_INET) {
        struct sockaddr_in sa = {};
        bpf_probe_read_user(&sa, sizeof(sa), uaddr);
        __u32 ip = bpf_ntohl(sa.sin_addr.s_addr);
        __u16 port = bpf_ntohs(sa.sin_port);
        pos = put_u8_dec(e->arg, pos, (ip >> 24) & 0xff);
        PUT(e->arg, pos, '.');
        pos = put_u8_dec(e->arg, pos, (ip >> 16) & 0xff);
        PUT(e->arg, pos, '.');
        pos = put_u8_dec(e->arg, pos, (ip >> 8) & 0xff);
        PUT(e->arg, pos, '.');
        pos = put_u8_dec(e->arg, pos, ip & 0xff);
        PUT(e->arg, pos, ':');
        pos = put_u16_dec(e->arg, pos, port);
    } else {
        struct sockaddr_in6 sa6 = {};
        bpf_probe_read_user(&sa6, sizeof(sa6), uaddr);
        __u16 port = bpf_ntohs(sa6.sin6_port);
        PUT(e->arg, pos, '[');
#pragma unroll
        for (int i = 0; i < 16; i++) {
            __u8 b = sa6.sin6_addr.in6_u.u6_addr8[i];
            PUT(e->arg, pos, hexd[b >> 4]);
            PUT(e->arg, pos, hexd[b & 0xf]);
            if ((i & 1) && i != 15)
                PUT(e->arg, pos, ':');
        }
        PUT(e->arg, pos, ']');
        PUT(e->arg, pos, ':');
        pos = put_u16_dec(e->arg, pos, port);
    }
    e->arg[pos & ARG_MASK] = 0;
    bpf_ringbuf_submit(e, 0);
    return 0;
}

SEC("tracepoint/syscalls/sys_enter_execve")
int trace_execve(struct trace_event_raw_sys_enter *ctx)
{
    __u32 tgid;
    if (!watched(&tgid))
        return 0;

    struct event *e = reserve_event(ctx, tgid, EVENT_PROC_EXEC);
    if (!e)
        return 0;
    const char *filename = (const char *)ctx->args[0];
    bpf_probe_read_user_str(&e->arg, sizeof(e->arg), filename);
    bpf_ringbuf_submit(e, 0);
    return 0;
}

SEC("lsm/file_open")
int enforce_file_open(struct file *file)
{
    if (!enforce_active() || !in_enforced_cgroup())
        return 0;

    __u32 tgid = bpf_get_current_pid_tgid() >> 32;
    struct deny_path key = {};
    long n = bpf_d_path(&file->f_path, key.path, sizeof(key.path));
    if (n <= 0)
        return 0;

    if (bpf_map_lookup_elem(&deny_open, &key))
        return deny_return((void *)file, tgid, EVENT_DENY_FILE_OPEN, key.path, n > PATH_MAX_LEN ? PATH_MAX_LEN : (int)n);
    return 0;
}

SEC("lsm/socket_connect")
int enforce_socket_connect(struct socket *sock, struct sockaddr *address, int addrlen)
{
    if (!enforce_active() || !in_enforced_cgroup())
        return 0;

    __u32 tgid = bpf_get_current_pid_tgid() >> 32;
    __u16 family = 0;
    bpf_probe_read_kernel(&family, sizeof(family), &address->sa_family);
    if (family != AF_INET && family != AF_INET6)
        return 0;

    struct deny_addr key = {};
    key.family = (__u8)family;
    char arg_buf[PATH_MAX_LEN] = {};
    int pos = 0;

    if (family == AF_INET) {
        struct sockaddr_in sa = {};
        bpf_probe_read_kernel(&sa, sizeof(sa), address);
        __u32 ip = bpf_ntohl(sa.sin_addr.s_addr);
        __u16 port = bpf_ntohs(sa.sin_port);
        key.port = port;
        __builtin_memcpy(key.addr, &sa.sin_addr.s_addr, 4);
        pos = put_u8_dec(arg_buf, pos, (ip >> 24) & 0xff);
        PUT(arg_buf, pos, '.');
        pos = put_u8_dec(arg_buf, pos, (ip >> 16) & 0xff);
        PUT(arg_buf, pos, '.');
        pos = put_u8_dec(arg_buf, pos, (ip >> 8) & 0xff);
        PUT(arg_buf, pos, '.');
        pos = put_u8_dec(arg_buf, pos, ip & 0xff);
        PUT(arg_buf, pos, ':');
        pos = put_u16_dec(arg_buf, pos, port);
    } else {
        struct sockaddr_in6 sa6 = {};
        bpf_probe_read_kernel(&sa6, sizeof(sa6), address);
        __u16 port = bpf_ntohs(sa6.sin6_port);
        key.port = port;
        __builtin_memcpy(key.addr, &sa6.sin6_addr, 16);
        PUT(arg_buf, pos, '[');
#pragma unroll
        for (int i = 0; i < 16; i++) {
            __u8 b = sa6.sin6_addr.in6_u.u6_addr8[i];
            PUT(arg_buf, pos, hexd[b >> 4]);
            PUT(arg_buf, pos, hexd[b & 0xf]);
            if ((i & 1) && i != 15)
                PUT(arg_buf, pos, ':');
        }
        PUT(arg_buf, pos, ']');
        PUT(arg_buf, pos, ':');
        pos = put_u16_dec(arg_buf, pos, port);
    }
    arg_buf[pos & ARG_MASK] = 0;

    if (bpf_map_lookup_elem(&deny_connect, &key))
        return deny_return((void *)sock, tgid, EVENT_DENY_CONNECT, arg_buf, pos);

    key.port = 0;
    if (bpf_map_lookup_elem(&deny_connect, &key))
        return deny_return((void *)sock, tgid, EVENT_DENY_CONNECT, arg_buf, pos);
    return 0;
}

SEC("lsm/bprm_check_security")
int enforce_bprm_check(struct linux_binprm *bprm)
{
    if (!enforce_active() || !in_enforced_cgroup())
        return 0;

    __u32 tgid = bpf_get_current_pid_tgid() >> 32;
    struct deny_path key = {};
    const char *filename = NULL;
    bpf_core_read(&filename, sizeof(filename), &bprm->filename);
    if (!filename)
        return 0;
    long n = bpf_probe_read_kernel_str(key.path, sizeof(key.path), filename);
    if (n <= 0)
        return 0;

    if (bpf_map_lookup_elem(&deny_exec, &key))
        return deny_return((void *)bprm, tgid, EVENT_DENY_EXEC, key.path, n > PATH_MAX_LEN ? PATH_MAX_LEN : (int)n);
    return 0;
}
