//! Accepts connections and relays bytes to the upstream, applying faults.
//!
//! Each direction of a connection is a small pipeline: a reader stamps every
//! chunk with the time it may leave (`read time + delay`) and queues it; a
//! writer sleeps until that time and writes it. So latency is paid once per
//! chunk *in flight*, as on a real network, not once per chunk in sequence —
//! a 100-chunk query result at 50 ms latency arrives after ~50 ms, not 5 s.
//!
//! Chunks never overtake each other: TCP delivers bytes in order, so a chunk
//! that rolled a short delay still waits for the one before it.

use std::io;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Duration;

use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::tcp::{OwnedReadHalf, OwnedWriteHalf};
use tokio::net::{TcpListener, TcpStream};
use tokio::sync::mpsc;
use tokio::time::{Instant, sleep, sleep_until};

use crate::faults::{Dice, Faults};

/// Bytes per read. Also the largest unit a fault applies to.
const CHUNK: usize = 16 * 1024;
/// Chunks queued per direction before the reader stops reading. A bound
/// gives backpressure: when the writer is held back, the proxy stops
/// draining the sender's socket and TCP flow control slows the sender down.
const IN_FLIGHT: usize = 64;

#[derive(Clone, Debug)]
pub struct Config {
    pub upstream: String,
    pub faults: Faults,
    /// Base seed; connection N uses seed + N, so a run can be replayed.
    pub seed: u64,
    /// Log every chunk, not just connects and faults.
    pub verbose: bool,
}

/// Accepts connections forever, relaying each one to `cfg.upstream`.
pub async fn serve(listener: TcpListener, cfg: Config) {
    let next_id = AtomicU64::new(0);
    loop {
        let (client, peer) = match listener.accept().await {
            Ok(c) => c,
            Err(e) => {
                // e.g. EMFILE (out of file descriptors): back off, keep serving.
                eprintln!("[!] accept: {e}");
                sleep(Duration::from_millis(100)).await;
                continue;
            }
        };
        let id = next_id.fetch_add(1, Ordering::Relaxed);
        let cfg = cfg.clone();
        tokio::spawn(async move {
            println!("[+] {peer} connected (conn {id})");
            match TcpStream::connect(&cfg.upstream).await {
                Ok(server) => {
                    if let Err(e) = relay(client, server, &cfg, id).await {
                        eprintln!("[!] {peer}: {e}");
                    }
                }
                Err(e) => eprintln!("[!] {peer}: cannot reach upstream {}: {e}", cfg.upstream),
            }
            println!("[-] {peer} disconnected");
        });
    }
}

async fn relay(client: TcpStream, server: TcpStream, cfg: &Config, id: u64) -> io::Result<()> {
    // Disable Nagle's algorithm: it holds small writes back while earlier
    // data is unacknowledged, which would add latency we did not choose.
    client.set_nodelay(true)?;
    server.set_nodelay(true)?;

    let (mut client_r, mut client_w) = client.into_split();
    let (mut server_r, mut server_w) = server.into_split();
    let seed = cfg.seed.wrapping_add(id.wrapping_mul(2));

    let up = direction(
        &mut client_r,
        &mut server_w,
        Dice::new(cfg.faults.clone(), seed),
        cfg,
        "client -> server",
    );
    let down = direction(
        &mut server_r,
        &mut client_w,
        Dice::new(cfg.faults.clone(), seed + 1),
        cfg,
        "server -> client",
    );

    // A side closing its half (FIN) ends one direction; the other keeps
    // flowing until its sender closes too — a client that sends a request
    // then shuts down writing still gets the whole response. An error ends
    // the connection at once: the other direction is dropped, closing both.
    tokio::pin!(up, down);
    tokio::select! {
        r = &mut up => { r?; down.await }
        r = &mut down => { r?; up.await }
    }
}

/// Relays one direction until the sender closes it, then closes it onward.
async fn direction(
    from: &mut OwnedReadHalf,
    to: &mut OwnedWriteHalf,
    mut dice: Dice,
    cfg: &Config,
    label: &str,
) -> io::Result<()> {
    let (tx, mut rx) = mpsc::channel::<(Instant, Vec<u8>)>(IN_FLIGHT);

    let reader = async move {
        let mut buf = vec![0u8; CHUNK];
        let mut prev_due = Instant::now();
        loop {
            let n = from.read(&mut buf).await?;
            if n == 0 {
                return Ok::<(), io::Error>(()); // EOF: dropping tx lets the writer drain and finish
            }
            let due = (Instant::now() + dice.delay()).max(prev_due);
            prev_due = due;
            if tx.send((due, buf[..n].to_vec())).await.is_err() {
                return Ok(()); // writer stopped (it reports why)
            }
        }
    };

    let writer = async move {
        while let Some((due, data)) = rx.recv().await {
            sleep_until(due).await;
            to.write_all(&data).await?;
            if cfg.verbose {
                println!("    {label}: {} bytes", data.len());
            }
        }
        to.shutdown().await // forward the FIN
    };

    // Reader done (EOF) → let the writer drain what's queued. Writer failed
    // → stop reading too. The writer can't finish first otherwise: the
    // channel stays open while the reader holds `tx`.
    tokio::pin!(writer);
    tokio::select! {
        r = reader => { r?; (&mut writer).await }
        w = &mut writer => w,
    }
}
