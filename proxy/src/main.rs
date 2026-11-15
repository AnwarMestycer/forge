//! forge-proxy — TCP fault-injection proxy.
//!
//!   forge-proxy 127.0.0.1:6544 127.0.0.1:5432 200          # 200 ms latency
//!   forge-proxy 127.0.0.1:6544 127.0.0.1:5432 --latency-ms 50 --jitter-ms 30
//!   forge-proxy 127.0.0.1:6544 127.0.0.1:5432 --loss 0.02 --reset 0.001
//!   forge-proxy 127.0.0.1:6544 127.0.0.1:5432 --bandwidth 64K

use std::time::Duration;

use clap::Parser;
use tokio::net::TcpListener;

use forge_proxy::faults::{Faults, parse_bandwidth};
use forge_proxy::relay::{self, Config};

#[derive(Parser)]
#[command(version, about = "TCP proxy that injects network faults")]
struct Args {
    /// Address to listen on.
    #[arg(default_value = "127.0.0.1:6544")]
    listen: String,
    /// Address to forward to.
    #[arg(default_value = "127.0.0.1:5432")]
    upstream: String,
    /// One-way latency in ms (positional form, kept for the spike/testbed).
    #[arg(value_name = "LATENCY_MS")]
    latency_pos: Option<u64>,

    /// One-way latency added to every chunk, in ms.
    #[arg(long, conflicts_with = "latency_pos")]
    latency_ms: Option<u64>,
    /// Each chunk's latency varies by up to ± this many ms.
    #[arg(long, default_value_t = 0)]
    jitter_ms: u64,
    /// Probability (0–1) that a chunk is lost: it stalls for a TCP
    /// retransmission timeout, doubling on repeat losses; 6 in a row resets.
    #[arg(long, default_value_t = 0.0, value_parser = probability)]
    loss: f64,
    /// First retransmission timeout for --loss, in ms (Linux's minimum is 200).
    #[arg(long, default_value_t = 200)]
    rto_ms: u64,
    /// Probability (0–1) per chunk that the connection is reset (RST).
    #[arg(long, default_value_t = 0.0, value_parser = probability)]
    reset: f64,
    /// Throughput cap per direction, bytes/s: 500, 64K (KiB/s), 1.5M (MiB/s).
    #[arg(long, value_parser = parse_bandwidth)]
    bandwidth: Option<u64>,
    /// Seed for the fault dice; the same seed replays the same faults.
    #[arg(long)]
    seed: Option<u64>,
    /// Log every chunk relayed.
    #[arg(short, long)]
    verbose: bool,
}

#[tokio::main]
async fn main() -> std::io::Result<()> {
    let args = Args::parse();
    let latency_ms = args.latency_ms.or(args.latency_pos).unwrap_or(0);
    let faults = Faults {
        latency: Duration::from_millis(latency_ms),
        jitter: Duration::from_millis(args.jitter_ms),
        loss: args.loss,
        rto: Duration::from_millis(args.rto_ms),
        reset: args.reset,
        bandwidth: args.bandwidth,
    };
    let seed = args.seed.unwrap_or_else(rand::random);

    let listener = TcpListener::bind(&args.listen).await?;
    println!("forge-proxy: {} -> {}", args.listen, args.upstream);
    println!(
        "  latency {latency_ms} ms ± {} ms, loss {} (rto {} ms), reset {}, bandwidth {}, seed {seed}",
        args.jitter_ms,
        args.loss,
        args.rto_ms,
        args.reset,
        args.bandwidth
            .map_or("unlimited".to_string(), |b| format!("{b} B/s")),
    );

    relay::serve(
        listener,
        Config {
            upstream: args.upstream,
            faults,
            seed,
            verbose: args.verbose,
        },
    )
    .await;
    Ok(())
}

fn probability(s: &str) -> Result<f64, String> {
    match s.parse::<f64>() {
        Ok(p) if (0.0..=1.0).contains(&p) => Ok(p),
        _ => Err(format!("{s:?} is not a probability between 0 and 1")),
    }
}
