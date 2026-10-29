//! forge-proxy — week-1 spike.
//!
//! A TCP proxy that forwards every connection to an upstream (e.g. Postgres)
//! and delays each chunk of data by a fixed amount, in both directions.
//!
//! Usage:
//!   forge-proxy [LISTEN_ADDR] [UPSTREAM_ADDR] [DELAY_MS]
//!   forge-proxy 127.0.0.1:6544 127.0.0.1:5432 200

use std::env;
use std::time::Duration;

use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::tcp::{OwnedReadHalf, OwnedWriteHalf};
use tokio::net::{TcpListener, TcpStream};
use tokio::time::sleep;

#[tokio::main]
async fn main() -> std::io::Result<()> {
    let args: Vec<String> = env::args().collect();
    let listen = args.get(1).cloned().unwrap_or_else(|| "127.0.0.1:6544".into());
    let upstream = args.get(2).cloned().unwrap_or_else(|| "127.0.0.1:5432".into());
    let delay_ms: u64 = args.get(3).and_then(|s| s.parse().ok()).unwrap_or(200);
    let delay = Duration::from_millis(delay_ms);

    let listener = TcpListener::bind(&listen).await?;
    println!("forge-proxy: {listen} -> {upstream} (delay {delay_ms} ms)");

    loop {
        let (client, peer) = listener.accept().await?;
        let upstream = upstream.clone();
        tokio::spawn(async move {
            println!("[+] {peer} connected");
            match TcpStream::connect(&upstream).await {
                Ok(server) => {
                    if let Err(e) = proxy(client, server, delay).await {
                        eprintln!("[!] {peer}: {e}");
                    }
                }
                Err(e) => eprintln!("[!] {peer}: cannot reach upstream {upstream}: {e}"),
            }
            println!("[-] {peer} disconnected");
        });
    }
}

/// Pipes data both ways until either side closes.
async fn proxy(client: TcpStream, server: TcpStream, delay: Duration) -> std::io::Result<()> {
    let (client_r, client_w) = client.into_split();
    let (server_r, server_w) = server.into_split();

    let up = pipe(client_r, server_w, delay, "client -> server");
    let down = pipe(server_r, client_w, delay, "server -> client");

    // Stop as soon as one direction finishes (connection closed).
    tokio::select! {
        r = up => r,
        r = down => r,
    }
}

/// Reads chunks from `from`, waits `delay`, writes them to `to`.
async fn pipe(
    mut from: OwnedReadHalf,
    mut to: OwnedWriteHalf,
    delay: Duration,
    label: &str,
) -> std::io::Result<()> {
    let mut buf = vec![0u8; 16 * 1024];
    loop {
        let n = from.read(&mut buf).await?;
        if n == 0 {
            to.shutdown().await.ok();
            return Ok(());
        }
        sleep(delay).await; // the "chaos": fixed latency per chunk
        to.write_all(&buf[..n]).await?;
        println!("    {label}: {n} bytes");
    }
}
