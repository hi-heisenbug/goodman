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
