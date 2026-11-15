//! forge-proxy — TCP fault-injection proxy.
//!
//!   forge-proxy 127.0.0.1:6544 127.0.0.1:5432 200          # 200 ms latency
//!   forge-proxy 127.0.0.1:6544 127.0.0.1:5432 --latency-ms 50 --jitter-ms 30

use std::time::Duration;

use clap::Parser;
use tokio::net::TcpListener;

use forge_proxy::faults::Faults;
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
    };
    let seed = args.seed.unwrap_or_else(rand::random);

    let listener = TcpListener::bind(&args.listen).await?;
    println!("forge-proxy: {} -> {}", args.listen, args.upstream);
    println!(
        "  latency {latency_ms} ms ± {} ms, seed {seed}",
        args.jitter_ms
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
