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

use crate::faults::{Dice, Fate, Faults};

/// How one direction of a connection ended.
#[derive(Debug, PartialEq)]
enum End {
    /// The sender closed it (FIN), and that was passed on.
    Closed,
    /// A fault reset the connection.
    Reset,
}

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

    // Scoped so the futures (and their borrows of the halves) are gone
    // before a reset needs the sockets.
    let end = {
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
            Dice::new(cfg.faults.clone(), seed.wrapping_add(1)),
            cfg,
            "server -> client",
        );

        // A side closing its half (FIN) ends one direction; the other keeps
        // flowing until its sender closes too — a client that sends a
        // request then shuts down writing still gets the whole response. A
        // reset or an error ends the connection at once.
        tokio::pin!(up, down);
        tokio::select! {
            r = &mut up => match r { Ok(End::Closed) => down.await, other => other },
            r = &mut down => match r { Ok(End::Closed) => up.await, other => other },
        }
    };

    match end {
        Ok(End::Closed) => Ok(()),
        Ok(End::Reset) => {
            println!("[~] conn {id}: reset injected");
            abort(client_r, client_w);
            abort(server_r, server_w);
            Ok(())
        }
        Err(e) => {
            // One side failed (often: it reset us). Reset the other rather
            // than leave it waiting on a connection that is gone.
            abort(client_r, client_w);
            abort(server_r, server_w);
            Err(e)
        }
    }
}

/// Closes the connection with a reset (RST) instead of a FIN.
///
/// With `SO_LINGER` on and a timeout of 0, `close()` discards unsent data and
/// sends RST; the peer's next read or write fails with ECONNRESET
/// ("connection reset by peer"). The write half is `forget`-ed because
/// dropping it normally calls `shutdown(SHUT_WR)`, which would send a FIN
/// first — the peer would see a clean EOF before the reset.
fn abort(r: OwnedReadHalf, w: OwnedWriteHalf) {
    if let Err(e) = socket2::SockRef::from(r.as_ref()).set_linger(Some(Duration::ZERO)) {
        eprintln!("[!] cannot set SO_LINGER for a reset: {e}");
    }
    w.forget();
    drop(r); // last handle: the socket closes here, with RST
}

/// Relays one direction until the sender closes it, then closes it onward.
async fn direction(
    from: &mut OwnedReadHalf,
    to: &mut OwnedWriteHalf,
    mut dice: Dice,
    cfg: &Config,
    label: &str,
) -> io::Result<End> {
    let (tx, mut rx) = mpsc::channel::<(Instant, Vec<u8>)>(IN_FLIGHT);

    let reader = async move {
        let mut buf = vec![0u8; CHUNK];
        let mut prev_due = Instant::now();
        loop {
            let n = from.read(&mut buf).await?;
            if n == 0 {
                return Ok::<End, io::Error>(End::Closed); // EOF: dropping tx lets the writer drain and finish
            }
            let now = Instant::now();
            let delay = match dice.roll() {
                Fate::Deliver { delay, retransmits } => {
                    if retransmits > 0 && cfg.verbose {
                        println!("    {label}: chunk lost {retransmits}× (+{delay:?})");
                    }
                    delay
                }
                Fate::Reset { after } => {
                    // Chunks already queued still go out meanwhile.
                    sleep(after).await;
                    return Ok(End::Reset);
                }
            };
            let due = (now + delay).max(prev_due);
            prev_due = due;
            if tx.send((due, buf[..n].to_vec())).await.is_err() {
                return Ok(End::Closed); // writer stopped (it reports why)
            }
        }
    };

    let writer = async move {
        let mut pace = Pace::new(cfg.faults.bandwidth);
        while let Some((due, data)) = rx.recv().await {
            sleep_until(due).await;
            pace.write(to, &data).await?;
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
        r = reader => match r? {
            End::Closed => (&mut writer).await.map(|()| End::Closed),
            End::Reset => Ok(End::Reset),
        },
        w = &mut writer => w.map(|()| End::Closed),
    }
}

/// Writes no faster than a fixed rate, in small slices so the flow is even.
///
/// When the writer falls behind, the bounded queue fills, the reader stops
/// reading, the kernel's receive buffer fills, and TCP's flow control
/// (the advertised window drops to zero) makes the sender wait: a slow link
/// slows the sender down instead of the proxy buffering without limit.
struct Pace {
    rate: Option<u64>,
    next: Instant,
}

impl Pace {
    fn new(rate: Option<u64>) -> Self {
        Self {
            rate,
            next: Instant::now(),
        }
    }

    async fn write(&mut self, to: &mut OwnedWriteHalf, data: &[u8]) -> io::Result<()> {
        let Some(rate) = self.rate else {
            return to.write_all(data).await;
        };
        // ~20 ms worth of bytes per write.
        let slice = (rate / 50).clamp(1, CHUNK as u64) as usize;
        for piece in data.chunks(slice) {
            sleep_until(self.next).await;
            let start = Instant::now();
            to.write_all(piece).await?;
            self.next = start + Duration::from_secs_f64(piece.len() as f64 / rate as f64);
        }
        Ok(())
    }
}
