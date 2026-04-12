//! Benchmarks for the SPSC ring buffer.
//!
//! These benchmarks measure the throughput and performance characteristics
//! of the lock-free ring buffer used for PTY output capture.

use criterion::{black_box, criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};

use clai_wrap::SpscRingBuffer;

/// Benchmark small sequential writes.
fn bench_small_writes(c: &mut Criterion) {
    let mut group = c.benchmark_group("ring_buffer_small_writes");

    for size in &[64, 256, 1024, 4096] {
        group.throughput(Throughput::Bytes(*size as u64));
        group.bench_with_input(BenchmarkId::from_parameter(size), size, |b, &size| {
            let mut buffer = SpscRingBuffer::new(1024 * 1024); // 1MB buffer
            let data = vec![b'x'; size];
            b.iter(|| {
                buffer.push(black_box(&data));
            });
        });
    }

    group.finish();
}

/// Benchmark larger bulk writes.
fn bench_bulk_writes(c: &mut Criterion) {
    let mut group = c.benchmark_group("ring_buffer_bulk_writes");

    for size in &[4 * 1024, 16 * 1024, 64 * 1024, 256 * 1024] {
        group.throughput(Throughput::Bytes(*size as u64));
        group.bench_with_input(BenchmarkId::from_parameter(size), size, |b, &size| {
            let mut buffer = SpscRingBuffer::new(2 * 1024 * 1024); // 2MB buffer
            let data = vec![b'x'; size];
            b.iter(|| {
                buffer.push(black_box(&data));
            });
        });
    }

    group.finish();
}

/// Benchmark write followed by drain cycle (typical usage pattern).
fn bench_write_drain_cycle(c: &mut Criterion) {
    let mut group = c.benchmark_group("ring_buffer_write_drain_cycle");

    for size in &[1024, 4096, 16384, 65536] {
        group.throughput(Throughput::Bytes(*size as u64));
        group.bench_with_input(BenchmarkId::from_parameter(size), size, |b, &size| {
            let mut buffer = SpscRingBuffer::new(1024 * 1024);
            let data = vec![b'x'; size];
            b.iter(|| {
                buffer.push(black_box(&data));
                black_box(buffer.drain());
            });
        });
    }

    group.finish();
}

/// Benchmark overflow behavior (writes exceeding capacity).
fn bench_overflow_writes(c: &mut Criterion) {
    let mut group = c.benchmark_group("ring_buffer_overflow");

    // Test with small buffer to force overflow
    let buffer_capacity = 64 * 1024; // 64KB

    for write_size in &[32 * 1024, 64 * 1024, 128 * 1024] {
        group.throughput(Throughput::Bytes(*write_size as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(write_size),
            write_size,
            |b, &write_size| {
                let mut buffer = SpscRingBuffer::new(buffer_capacity);
                let data = vec![b'x'; write_size];
                b.iter(|| {
                    buffer.push(black_box(&data));
                });
            },
        );
    }

    group.finish();
}

/// Benchmark multiple small writes accumulating (simulates many small PTY reads).
fn bench_accumulated_small_writes(c: &mut Criterion) {
    let mut group = c.benchmark_group("ring_buffer_accumulated_writes");

    // Simulate 100 small writes then drain
    let num_writes = 100;
    for chunk_size in &[64, 128, 256, 512] {
        let total_bytes = *chunk_size * num_writes;
        group.throughput(Throughput::Bytes(total_bytes as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(chunk_size),
            chunk_size,
            |b, &chunk_size| {
                let mut buffer = SpscRingBuffer::new(1024 * 1024);
                let data = vec![b'x'; chunk_size];
                b.iter(|| {
                    for _ in 0..num_writes {
                        buffer.push(black_box(&data));
                    }
                    black_box(buffer.drain());
                });
            },
        );
    }

    group.finish();
}

/// Benchmark drain of various sizes (measures consumer performance).
fn bench_drain_sizes(c: &mut Criterion) {
    let mut group = c.benchmark_group("ring_buffer_drain");

    for size in &[1024, 4096, 16_384, 65_536, 262_144] {
        group.throughput(Throughput::Bytes(*size as u64));
        group.bench_with_input(BenchmarkId::from_parameter(size), size, |b, &size| {
            b.iter_batched(
                || {
                    let mut buffer = SpscRingBuffer::new(1024 * 1024);
                    let data = vec![b'x'; size];
                    buffer.push(&data);
                    buffer
                },
                |mut buffer| black_box(buffer.drain()),
                criterion::BatchSize::SmallInput,
            );
        });
    }

    group.finish();
}

/// Benchmark typical PTY output pattern (mixed small/medium writes).
fn bench_pty_pattern(c: &mut Criterion) {
    c.bench_function("ring_buffer_pty_pattern", |b| {
        let mut buffer = SpscRingBuffer::new(2 * 1024 * 1024);

        // Simulate a mix of write sizes typical in PTY output
        let small_write = vec![b'x'; 80]; // Single line
        let medium_write = vec![b'x'; 1024]; // Block of output
        let large_write = vec![b'x'; 8192]; // Large chunk (e.g., file listing)

        b.iter(|| {
            // Typical pattern: many small writes, some medium, few large
            for _ in 0..10 {
                buffer.push(black_box(&small_write));
            }
            for _ in 0..3 {
                buffer.push(black_box(&medium_write));
            }
            buffer.push(black_box(&large_write));
            black_box(buffer.drain());
        });
    });
}

/// Benchmark checking overflow flag (should be very fast).
fn bench_overflow_check(c: &mut Criterion) {
    c.bench_function("ring_buffer_overflow_check", |b| {
        let buffer = SpscRingBuffer::new(1024);
        b.iter(|| black_box(buffer.has_overflowed()));
    });
}

/// Benchmark buffer status queries.
fn bench_status_queries(c: &mut Criterion) {
    let mut group = c.benchmark_group("ring_buffer_status");

    // Pre-fill buffer for realistic measurements
    let mut buffer = SpscRingBuffer::new(1024 * 1024);
    let data = vec![b'x'; 512 * 1024];
    buffer.push(&data);

    group.bench_function("len", |b| {
        b.iter(|| black_box(buffer.len()));
    });

    group.bench_function("is_empty", |b| {
        b.iter(|| black_box(buffer.is_empty()));
    });

    group.bench_function("capacity", |b| {
        b.iter(|| black_box(buffer.capacity()));
    });

    group.finish();
}

criterion_group!(
    benches,
    bench_small_writes,
    bench_bulk_writes,
    bench_write_drain_cycle,
    bench_overflow_writes,
    bench_accumulated_small_writes,
    bench_drain_sizes,
    bench_pty_pattern,
    bench_overflow_check,
    bench_status_queries,
);

criterion_main!(benches);
