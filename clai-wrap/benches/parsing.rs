//! Benchmarks for OSC 133 parsing and hotkey detection.
//!
//! These benchmarks measure the performance of escape sequence parsing
//! and hotkey chord detection, which are critical for low-latency operation.

use criterion::{black_box, criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};

use clai_wrap::hotkey::{HotkeyConfig, HotkeyParser, CHORD_FIRST_BYTE};
use clai_wrap::Osc133Parser;

// =============================================================================
// OSC 133 Parsing Benchmarks
// =============================================================================

/// Benchmark parsing a simple OSC 133 prompt sequence.
fn bench_osc133_simple_sequence(c: &mut Criterion) {
    let mut group = c.benchmark_group("osc133_simple");

    // OSC 133;A (prompt)
    let prompt_seq = b"\x1b]133;A\x07";
    group.throughput(Throughput::Bytes(prompt_seq.len() as u64));
    group.bench_function("prompt", |b| {
        let mut parser = Osc133Parser::new();
        b.iter(|| {
            parser.process_bytes(black_box(prompt_seq));
        });
    });

    // OSC 133;B (input)
    let input_seq = b"\x1b]133;B\x07";
    group.throughput(Throughput::Bytes(input_seq.len() as u64));
    group.bench_function("input", |b| {
        let mut parser = Osc133Parser::new();
        b.iter(|| {
            parser.process_bytes(black_box(input_seq));
        });
    });

    // OSC 133;C (output)
    let output_seq = b"\x1b]133;C\x07";
    group.throughput(Throughput::Bytes(output_seq.len() as u64));
    group.bench_function("output", |b| {
        let mut parser = Osc133Parser::new();
        b.iter(|| {
            parser.process_bytes(black_box(output_seq));
        });
    });

    // OSC 133;D;0 (finished with exit code)
    let finished_seq = b"\x1b]133;D;0\x07";
    group.throughput(Throughput::Bytes(finished_seq.len() as u64));
    group.bench_function("finished", |b| {
        let mut parser = Osc133Parser::new();
        b.iter(|| {
            parser.process_bytes(black_box(finished_seq));
        });
    });

    group.finish();
}

/// Benchmark parsing OSC 133 with ST terminator (escape backslash).
fn bench_osc133_st_terminator(c: &mut Criterion) {
    let mut group = c.benchmark_group("osc133_st_terminator");

    let prompt_seq = b"\x1b]133;A\x1b\\";
    group.throughput(Throughput::Bytes(prompt_seq.len() as u64));
    group.bench_function("prompt_st", |b| {
        let mut parser = Osc133Parser::new();
        b.iter(|| {
            parser.process_bytes(black_box(prompt_seq));
        });
    });

    let finished_seq = b"\x1b]133;D;127\x1b\\";
    group.throughput(Throughput::Bytes(finished_seq.len() as u64));
    group.bench_function("finished_st", |b| {
        let mut parser = Osc133Parser::new();
        b.iter(|| {
            parser.process_bytes(black_box(finished_seq));
        });
    });

    group.finish();
}

/// Benchmark parsing split OSC 133 sequences (split across reads).
fn bench_osc133_split_sequence(c: &mut Criterion) {
    c.bench_function("osc133_split_packet", |b| {
        let mut parser = Osc133Parser::new();
        let part1 = b"\x1b]";
        let part2 = b"133;";
        let part3 = b"A\x07";
        b.iter(|| {
            parser.process_bytes(black_box(part1));
            parser.process_bytes(black_box(part2));
            parser.process_bytes(black_box(part3));
        });
    });
}

/// Benchmark parsing OSC 133 interleaved with regular terminal output.
fn bench_osc133_interleaved(c: &mut Criterion) {
    let mut group = c.benchmark_group("osc133_interleaved");

    // Simulate typical shell output with OSC sequences
    let short_output =
        b"\x1b]133;A\x07$ ls\x1b]133;B\x07\x1b]133;C\x07file1.txt\nfile2.txt\n\x1b]133;D;0\x07";
    group.throughput(Throughput::Bytes(short_output.len() as u64));
    group.bench_function("short_command", |b| {
        let mut parser = Osc133Parser::new();
        b.iter(|| {
            parser.process_bytes(black_box(short_output));
        });
    });

    // Longer output simulation
    let long_output: Vec<u8> = {
        let mut v = Vec::new();
        v.extend_from_slice(b"\x1b]133;A\x07$ find . -name '*.rs'\x1b]133;B\x07\x1b]133;C\x07");
        // Add 100 lines of output
        for i in 0..100 {
            v.extend_from_slice(format!("./path/to/file{i}.rs\n").as_bytes());
        }
        v.extend_from_slice(b"\x1b]133;D;0\x07");
        v
    };
    group.throughput(Throughput::Bytes(long_output.len() as u64));
    group.bench_function("long_output", |b| {
        let mut parser = Osc133Parser::new();
        b.iter(|| {
            parser.process_bytes(black_box(&long_output));
        });
    });

    group.finish();
}

/// Benchmark parsing plain text (no escape sequences) - baseline.
fn bench_osc133_plain_text(c: &mut Criterion) {
    let mut group = c.benchmark_group("osc133_plain_text");

    for size in &[100, 1000, 10000] {
        let plain_text: Vec<u8> = vec![b'x'; *size];
        group.throughput(Throughput::Bytes(*size as u64));
        group.bench_with_input(BenchmarkId::from_parameter(size), size, |b, _| {
            let mut parser = Osc133Parser::new();
            b.iter(|| {
                parser.process_bytes(black_box(&plain_text));
            });
        });
    }

    group.finish();
}

/// Benchmark parsing other OSC sequences (should be ignored).
fn bench_osc133_other_osc(c: &mut Criterion) {
    c.bench_function("osc133_ignore_other_osc", |b| {
        let mut parser = Osc133Parser::new();
        // OSC 0 (window title) - should be ignored
        let title_seq = b"\x1b]0;Terminal Title\x07";
        // Mix of OSC sequences
        let mixed = b"\x1b]0;title\x07\x1b]133;A\x07\x1b]52;c;dGVzdA==\x07";
        b.iter(|| {
            parser.process_bytes(black_box(title_seq));
            parser.process_bytes(black_box(mixed));
        });
    });
}

/// Benchmark state query (should be extremely fast).
fn bench_osc133_state_query(c: &mut Criterion) {
    c.bench_function("osc133_state_query", |b| {
        let parser = Osc133Parser::new();
        b.iter(|| black_box(parser.current_state()));
    });
}

// =============================================================================
// Hotkey Detection Benchmarks
// =============================================================================

/// Benchmark single byte processing in idle state.
fn bench_hotkey_idle_passthrough(c: &mut Criterion) {
    let mut group = c.benchmark_group("hotkey_idle");

    // Single normal byte
    group.bench_function("single_byte", |b| {
        let mut parser = HotkeyParser::new();
        b.iter(|| {
            black_box(parser.process_byte(black_box(b'a')));
        });
    });

    // Multiple bytes (simulating typing)
    let input = b"hello world";
    group.throughput(Throughput::Bytes(input.len() as u64));
    group.bench_function("typing", |b| {
        let mut parser = HotkeyParser::new();
        b.iter(|| {
            black_box(parser.process_bytes(black_box(input)));
        });
    });

    group.finish();
}

/// Benchmark successful chord detection.
fn bench_hotkey_chord_detection(c: &mut Criterion) {
    let mut group = c.benchmark_group("hotkey_chord");

    // Successful history chord (Ctrl-\ h)
    group.bench_function("history_chord", |b| {
        let mut parser = HotkeyParser::new();
        b.iter(|| {
            parser.process_byte(black_box(CHORD_FIRST_BYTE));
            black_box(parser.process_byte(black_box(b'h')));
        });
    });

    // Successful completions chord (Ctrl-\ c)
    group.bench_function("completions_chord", |b| {
        let mut parser = HotkeyParser::new();
        b.iter(|| {
            parser.process_byte(black_box(CHORD_FIRST_BYTE));
            black_box(parser.process_byte(black_box(b'c')));
        });
    });

    group.finish();
}

/// Benchmark chord cancellation scenarios.
fn bench_hotkey_chord_cancel(c: &mut Criterion) {
    let mut group = c.benchmark_group("hotkey_chord_cancel");

    // Cancel with escape
    group.bench_function("escape_cancel", |b| {
        let mut parser = HotkeyParser::new();
        b.iter(|| {
            parser.process_byte(black_box(CHORD_FIRST_BYTE));
            black_box(parser.process_byte(black_box(0x1B))); // ESC
        });
    });

    // Cancel with invalid second byte
    group.bench_function("invalid_second", |b| {
        let mut parser = HotkeyParser::new();
        b.iter(|| {
            parser.process_byte(black_box(CHORD_FIRST_BYTE));
            black_box(parser.process_byte(black_box(b'x')));
        });
    });

    group.finish();
}

/// Benchmark mixed input with occasional chords.
fn bench_hotkey_mixed_input(c: &mut Criterion) {
    c.bench_function("hotkey_mixed_input", |b| {
        let mut parser = HotkeyParser::new();
        // Simulate typing with occasional chord trigger
        let input: Vec<u8> = {
            let mut v = Vec::new();
            v.extend_from_slice(b"git status");
            v.push(CHORD_FIRST_BYTE);
            v.push(b'h');
            v.extend_from_slice(b"ls -la");
            v.push(CHORD_FIRST_BYTE);
            v.push(b'c');
            v.extend_from_slice(b"echo hello");
            v
        };
        b.iter(|| {
            black_box(parser.process_bytes(black_box(&input)));
        });
    });
}

/// Benchmark `check_timeout` (called periodically).
fn bench_hotkey_timeout_check(c: &mut Criterion) {
    let mut group = c.benchmark_group("hotkey_timeout");

    // Check timeout when idle
    group.bench_function("idle", |b| {
        let mut parser = HotkeyParser::new();
        b.iter(|| {
            black_box(parser.check_timeout());
        });
    });

    // Check timeout when waiting for second byte
    group.bench_function("waiting", |b| {
        let mut parser = HotkeyParser::new();
        parser.process_byte(CHORD_FIRST_BYTE);
        b.iter(|| {
            black_box(parser.check_timeout());
        });
    });

    group.finish();
}

/// Benchmark custom config hotkey parser.
fn bench_hotkey_custom_config(c: &mut Criterion) {
    c.bench_function("hotkey_custom_config", |b| {
        let config = HotkeyConfig {
            timeout: std::time::Duration::from_millis(100),
            first_byte: b'@',
            history_byte: b'1',
            completions_byte: b'2',
        };
        let mut parser = HotkeyParser::with_config(config);
        b.iter(|| {
            parser.process_byte(black_box(b'@'));
            black_box(parser.process_byte(black_box(b'1')));
        });
    });
}

/// Benchmark reset operation.
fn bench_hotkey_reset(c: &mut Criterion) {
    let mut group = c.benchmark_group("hotkey_reset");

    // Reset from idle
    group.bench_function("from_idle", |b| {
        let mut parser = HotkeyParser::new();
        b.iter(|| {
            black_box(parser.reset());
        });
    });

    // Reset from waiting state
    group.bench_function("from_waiting", |b| {
        b.iter_batched(
            || {
                let mut parser = HotkeyParser::new();
                parser.process_byte(CHORD_FIRST_BYTE);
                parser
            },
            |mut parser| black_box(parser.reset()),
            criterion::BatchSize::SmallInput,
        );
    });

    group.finish();
}

/// Benchmark `is_waiting` query (called frequently).
fn bench_hotkey_is_waiting(c: &mut Criterion) {
    c.bench_function("hotkey_is_waiting", |b| {
        let parser = HotkeyParser::new();
        b.iter(|| black_box(parser.is_waiting()));
    });
}

// =============================================================================
// Combined Parsing Benchmarks
// =============================================================================

/// Benchmark realistic terminal stream processing.
fn bench_combined_parsing(c: &mut Criterion) {
    c.bench_function("combined_terminal_stream", |b| {
        let mut osc_parser = Osc133Parser::new();
        let mut hotkey_parser = HotkeyParser::new();

        // Simulate realistic terminal I/O: shell output with OSC sequences and user input
        let stream: Vec<u8> = {
            let mut v = Vec::new();
            // Prompt
            v.extend_from_slice(b"\x1b]133;A\x07$ ");
            // User types command (includes potential hotkey bytes)
            v.extend_from_slice(b"git status");
            // Command execution
            v.extend_from_slice(b"\x1b]133;B\x07\x1b]133;C\x07");
            // Output
            v.extend_from_slice(b"On branch main\nnothing to commit\n");
            // Finished
            v.extend_from_slice(b"\x1b]133;D;0\x07");
            // Next prompt
            v.extend_from_slice(b"\x1b]133;A\x07$ ");
            v
        };

        b.iter(|| {
            osc_parser.process_bytes(black_box(&stream));
            hotkey_parser.process_bytes(black_box(&stream));
        });
    });
}

criterion_group!(
    osc133_benches,
    bench_osc133_simple_sequence,
    bench_osc133_st_terminator,
    bench_osc133_split_sequence,
    bench_osc133_interleaved,
    bench_osc133_plain_text,
    bench_osc133_other_osc,
    bench_osc133_state_query,
);

criterion_group!(
    hotkey_benches,
    bench_hotkey_idle_passthrough,
    bench_hotkey_chord_detection,
    bench_hotkey_chord_cancel,
    bench_hotkey_mixed_input,
    bench_hotkey_timeout_check,
    bench_hotkey_custom_config,
    bench_hotkey_reset,
    bench_hotkey_is_waiting,
);

criterion_group!(combined_benches, bench_combined_parsing,);

criterion_main!(osc133_benches, hotkey_benches, combined_benches);
