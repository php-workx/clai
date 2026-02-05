//! Benchmarks for picker filtering and history file parsing.
//!
//! These benchmarks measure the performance of fuzzy filtering and
//! history file parsing, which impact the perceived responsiveness
//! of the picker UI.

use criterion::{black_box, criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};

use clai_wrap::history_parser::{
    parse_bash_history, parse_bash_timestamped, parse_fish_history, parse_zsh_history,
};
use clai_wrap::picker::{Picker, PickerItem};

// =============================================================================
// History Parsing Benchmarks
// =============================================================================

/// Generate sample bash history content.
fn generate_bash_history(num_entries: usize) -> String {
    let commands = [
        "ls -la",
        "git status",
        "git commit -m 'test message'",
        "cargo build --release",
        "cd /path/to/directory",
        "vim src/main.rs",
        "grep -r 'pattern' .",
        "docker ps -a",
        "kubectl get pods",
        "make test",
    ];

    (0..num_entries)
        .map(|i| commands[i % commands.len()])
        .collect::<Vec<_>>()
        .join("\n")
}

/// Generate sample bash timestamped history content.
fn generate_bash_timestamped_history(num_entries: usize) -> String {
    let commands = [
        "ls -la",
        "git status",
        "git commit -m 'test message'",
        "cargo build --release",
        "cd /path/to/directory",
        "vim src/main.rs",
        "grep -r 'pattern' .",
        "docker ps -a",
        "kubectl get pods",
        "make test",
    ];

    let base_timestamp = 1_700_000_000_i64;
    (0..num_entries)
        .map(|i| {
            format!(
                "#{}\n{}",
                base_timestamp + i as i64,
                commands[i % commands.len()]
            )
        })
        .collect::<Vec<_>>()
        .join("\n")
}

/// Generate sample zsh history content.
fn generate_zsh_history(num_entries: usize) -> String {
    let commands = [
        "ls -la",
        "git status",
        "git commit -m 'test message'",
        "cargo build --release",
        "cd /path/to/directory",
        "vim src/main.rs",
        "grep -r 'pattern' .",
        "docker ps -a",
        "kubectl get pods",
        "make test",
    ];

    let base_timestamp = 1_700_000_000_i64;
    (0..num_entries)
        .map(|i| {
            format!(
                ": {}:0;{}",
                base_timestamp + i as i64,
                commands[i % commands.len()]
            )
        })
        .collect::<Vec<_>>()
        .join("\n")
}

/// Generate sample fish history content.
fn generate_fish_history(num_entries: usize) -> String {
    let commands = [
        "ls -la",
        "git status",
        "git commit -m 'test message'",
        "cargo build --release",
        "cd /path/to/directory",
        "vim src/main.rs",
        "grep -r 'pattern' .",
        "docker ps -a",
        "kubectl get pods",
        "make test",
    ];

    let base_timestamp = 1_700_000_000_i64;
    (0..num_entries)
        .map(|i| {
            format!(
                "- cmd: {}\n  when: {}",
                commands[i % commands.len()],
                base_timestamp + i as i64
            )
        })
        .collect::<Vec<_>>()
        .join("\n")
}

/// Benchmark bash plain history parsing.
fn bench_parse_bash_history(c: &mut Criterion) {
    let mut group = c.benchmark_group("parse_bash_plain");

    for num_entries in [100, 500, 1000, 5000, 10000].iter() {
        let content = generate_bash_history(*num_entries);
        group.throughput(Throughput::Bytes(content.len() as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(num_entries),
            &content,
            |b, content| {
                b.iter(|| {
                    black_box(parse_bash_history(black_box(content)));
                });
            },
        );
    }

    group.finish();
}

/// Benchmark bash timestamped history parsing.
fn bench_parse_bash_timestamped(c: &mut Criterion) {
    let mut group = c.benchmark_group("parse_bash_timestamped");

    for num_entries in [100, 500, 1000, 5000, 10000].iter() {
        let content = generate_bash_timestamped_history(*num_entries);
        group.throughput(Throughput::Bytes(content.len() as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(num_entries),
            &content,
            |b, content| {
                b.iter(|| {
                    black_box(parse_bash_timestamped(black_box(content)));
                });
            },
        );
    }

    group.finish();
}

/// Benchmark zsh history parsing.
fn bench_parse_zsh_history(c: &mut Criterion) {
    let mut group = c.benchmark_group("parse_zsh_history");

    for num_entries in [100, 500, 1000, 5000, 10000].iter() {
        let content = generate_zsh_history(*num_entries);
        group.throughput(Throughput::Bytes(content.len() as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(num_entries),
            &content,
            |b, content| {
                b.iter(|| {
                    black_box(parse_zsh_history(black_box(content)));
                });
            },
        );
    }

    group.finish();
}

/// Benchmark fish history parsing.
fn bench_parse_fish_history(c: &mut Criterion) {
    let mut group = c.benchmark_group("parse_fish_history");

    for num_entries in [100, 500, 1000, 5000, 10000].iter() {
        let content = generate_fish_history(*num_entries);
        group.throughput(Throughput::Bytes(content.len() as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(num_entries),
            &content,
            |b, content| {
                b.iter(|| {
                    black_box(parse_fish_history(black_box(content)));
                });
            },
        );
    }

    group.finish();
}

// =============================================================================
// Picker Filtering Benchmarks
// =============================================================================

/// Generate sample picker items.
fn generate_picker_items(num_items: usize) -> Vec<PickerItem> {
    let commands = [
        "git status",
        "git commit -m 'fix: resolve issue'",
        "git push origin main",
        "git pull --rebase",
        "git diff HEAD~1",
        "cargo build --release",
        "cargo test",
        "cargo clippy",
        "cargo fmt --check",
        "make dev",
        "ls -la",
        "cd /path/to/project",
        "vim src/main.rs",
        "grep -r 'TODO' src/",
        "docker-compose up -d",
        "kubectl get pods -n production",
        "npm install",
        "python manage.py migrate",
        "ssh user@server.example.com",
        "curl -X POST https://api.example.com/data",
    ];

    (0..num_items)
        .map(|i| PickerItem::new(commands[i % commands.len()]))
        .collect()
}

/// Benchmark picker creation.
fn bench_picker_creation(c: &mut Criterion) {
    let mut group = c.benchmark_group("picker_creation");

    for num_items in [100, 500, 1000, 5000, 10000].iter() {
        let items = generate_picker_items(*num_items);
        group.bench_with_input(
            BenchmarkId::from_parameter(num_items),
            &items,
            |b, items| {
                b.iter(|| {
                    black_box(Picker::new(black_box(items.clone())));
                });
            },
        );
    }

    group.finish();
}

/// Benchmark picker filtering with various query lengths.
fn bench_picker_filter_query_length(c: &mut Criterion) {
    let mut group = c.benchmark_group("picker_filter_query_length");

    let items = generate_picker_items(5000);
    let queries = [
        ("1_char", "g"),
        ("2_chars", "gi"),
        ("3_chars", "git"),
        ("4_chars", "git "),
        ("5_chars", "git s"),
        ("long", "git commit"),
    ];

    for (name, query) in queries.iter() {
        group.bench_function(*name, |b| {
            let mut picker = Picker::new(items.clone());
            b.iter(|| {
                picker.update_query(black_box(*query));
            });
        });
    }

    group.finish();
}

/// Benchmark picker filtering with various item counts.
fn bench_picker_filter_item_count(c: &mut Criterion) {
    let mut group = c.benchmark_group("picker_filter_item_count");

    let query = "git";

    for num_items in [100, 500, 1000, 5000, 10000].iter() {
        let items = generate_picker_items(*num_items);
        group.bench_with_input(
            BenchmarkId::from_parameter(num_items),
            &items,
            |b, items| {
                let mut picker = Picker::new(items.clone());
                b.iter(|| {
                    picker.update_query(black_box(query));
                    picker.update_query(""); // Reset for next iteration
                });
            },
        );
    }

    group.finish();
}

/// Benchmark incremental filtering (typing character by character).
fn bench_picker_incremental_filter(c: &mut Criterion) {
    let items = generate_picker_items(5000);

    c.bench_function("picker_incremental_typing", |b| {
        let mut picker = Picker::new(items.clone());
        let query_chars: Vec<char> = "git commit".chars().collect();
        b.iter(|| {
            for c in &query_chars {
                picker.push_char(black_box(*c));
            }
            // Clear for next iteration
            for _ in 0..query_chars.len() {
                picker.pop_char();
            }
        });
    });
}

/// Benchmark filter with no matches.
fn bench_picker_filter_no_match(c: &mut Criterion) {
    let items = generate_picker_items(5000);

    c.bench_function("picker_filter_no_match", |b| {
        let mut picker = Picker::new(items.clone());
        let query = "xyznonexistent123";
        b.iter(|| {
            picker.update_query(black_box(query));
            picker.update_query(""); // Reset
        });
    });
}

/// Benchmark filter with all matches (empty query).
fn bench_picker_filter_all_match(c: &mut Criterion) {
    let items = generate_picker_items(5000);

    c.bench_function("picker_filter_all_match", |b| {
        let mut picker = Picker::new(items.clone());
        picker.update_query("git"); // First narrow down
        b.iter(|| {
            picker.update_query(black_box("")); // Show all
        });
    });
}

/// Benchmark case-insensitive matching.
fn bench_picker_case_insensitive(c: &mut Criterion) {
    let items: Vec<PickerItem> = (0..5000)
        .map(|i| {
            if i % 2 == 0 {
                PickerItem::new("Git Status")
            } else {
                PickerItem::new("GIT COMMIT")
            }
        })
        .collect();

    let queries = [("lower", "git"), ("upper", "GIT"), ("mixed", "GiT")];

    let mut group = c.benchmark_group("picker_case_insensitive");

    for (name, query) in queries.iter() {
        group.bench_function(*name, |b| {
            let mut picker = Picker::new(items.clone());
            b.iter(|| {
                picker.update_query(black_box(*query));
            });
        });
    }

    group.finish();
}

// =============================================================================
// Picker Navigation Benchmarks
// =============================================================================

/// Benchmark navigation operations.
fn bench_picker_navigation(c: &mut Criterion) {
    let items = generate_picker_items(5000);

    let mut group = c.benchmark_group("picker_navigation");

    group.bench_function("select_next", |b| {
        let mut picker = Picker::new(items.clone());
        b.iter(|| {
            picker.select_next();
        });
    });

    group.bench_function("select_prev", |b| {
        let mut picker = Picker::new(items.clone());
        b.iter(|| {
            picker.select_prev();
        });
    });

    group.bench_function("selected_item", |b| {
        let picker = Picker::new(items.clone());
        b.iter(|| {
            black_box(picker.selected_item());
        });
    });

    group.finish();
}

/// Benchmark combined filter and navigate pattern.
fn bench_picker_filter_and_navigate(c: &mut Criterion) {
    let items = generate_picker_items(5000);

    c.bench_function("picker_filter_then_navigate", |b| {
        let mut picker = Picker::new(items.clone());
        b.iter(|| {
            picker.update_query("git");
            for _ in 0..10 {
                picker.select_next();
            }
            for _ in 0..5 {
                picker.select_prev();
            }
            black_box(picker.selected_item());
            picker.update_query("");
        });
    });
}

// =============================================================================
// Picker Status Query Benchmarks
// =============================================================================

/// Benchmark status query operations.
fn bench_picker_status_queries(c: &mut Criterion) {
    let items = generate_picker_items(5000);
    let picker = Picker::new(items);

    let mut group = c.benchmark_group("picker_status");

    group.bench_function("filtered_count", |b| {
        b.iter(|| black_box(picker.filtered_count()));
    });

    group.bench_function("total_count", |b| {
        b.iter(|| black_box(picker.total_count()));
    });

    group.bench_function("is_empty", |b| {
        b.iter(|| black_box(picker.is_empty()));
    });

    group.bench_function("is_filtered_empty", |b| {
        b.iter(|| black_box(picker.is_filtered_empty()));
    });

    group.bench_function("query", |b| {
        b.iter(|| black_box(picker.query()));
    });

    group.finish();
}

// =============================================================================
// Realistic Scenario Benchmarks
// =============================================================================

/// Benchmark realistic picker usage pattern.
fn bench_picker_realistic_usage(c: &mut Criterion) {
    let items = generate_picker_items(10000);

    c.bench_function("picker_realistic_session", |b| {
        b.iter(|| {
            let mut picker = Picker::new(items.clone());

            // User types incrementally
            picker.push_char('g');
            picker.push_char('i');
            picker.push_char('t');

            // Navigate to find the right command
            for _ in 0..5 {
                picker.select_next();
            }

            // Refine search
            picker.push_char(' ');
            picker.push_char('c');

            // Navigate again
            picker.select_next();
            picker.select_next();

            // Get selection
            black_box(picker.selected_item());
        });
    });
}

/// Benchmark picker with long commands.
fn bench_picker_long_commands(c: &mut Criterion) {
    let items: Vec<PickerItem> = (0..5000)
        .map(|i| {
            PickerItem::new(format!(
                "git commit -m 'feat(module-{i}): implement feature with long description explaining the changes in detail'"
            ))
        })
        .collect();

    c.bench_function("picker_long_commands", |b| {
        let mut picker = Picker::new(items.clone());
        b.iter(|| {
            picker.update_query("feat");
            black_box(picker.filtered_count());
            picker.update_query("");
        });
    });
}

/// Benchmark picker with unicode content.
fn bench_picker_unicode(c: &mut Criterion) {
    let items: Vec<PickerItem> = (0..5000)
        .map(|i| PickerItem::new(format!("echo '\u{4e2d}\u{6587}' # Chinese text {i}")))
        .collect();

    c.bench_function("picker_unicode_content", |b| {
        let mut picker = Picker::new(items.clone());
        b.iter(|| {
            picker.update_query("\u{4e2d}");
            black_box(picker.filtered_count());
            picker.update_query("");
        });
    });
}

criterion_group!(
    history_parsing_benches,
    bench_parse_bash_history,
    bench_parse_bash_timestamped,
    bench_parse_zsh_history,
    bench_parse_fish_history,
);

criterion_group!(
    picker_filter_benches,
    bench_picker_creation,
    bench_picker_filter_query_length,
    bench_picker_filter_item_count,
    bench_picker_incremental_filter,
    bench_picker_filter_no_match,
    bench_picker_filter_all_match,
    bench_picker_case_insensitive,
);

criterion_group!(
    picker_navigation_benches,
    bench_picker_navigation,
    bench_picker_filter_and_navigate,
    bench_picker_status_queries,
);

criterion_group!(
    picker_realistic_benches,
    bench_picker_realistic_usage,
    bench_picker_long_commands,
    bench_picker_unicode,
);

criterion_main!(
    history_parsing_benches,
    picker_filter_benches,
    picker_navigation_benches,
    picker_realistic_benches,
);
