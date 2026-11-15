//! End-to-end: a real echo server behind a real proxy, on loopback.
//! Timing bounds are generous so a busy CI machine doesn't make them flaky.

use std::net::SocketAddr;
use std::time::Duration;

use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::{TcpListener, TcpStream};
use tokio::time::{Instant, sleep};

use forge_proxy::faults::Faults;
use forge_proxy::relay::{self, Config};

fn ms(n: u64) -> Duration {
    Duration::from_millis(n)
}

/// Echoes everything back, then closes when the client closes.
async fn echo_server() -> SocketAddr {
    let l = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = l.local_addr().unwrap();
    tokio::spawn(async move {
        loop {
            let Ok((mut s, _)) = l.accept().await else {
                return;
            };
            tokio::spawn(async move {
                let (mut r, mut w) = s.split();
                let _ = tokio::io::copy(&mut r, &mut w).await;
                let _ = w.shutdown().await;
            });
        }
    });
    addr
}

/// Starts a proxy in front of `upstream` and returns its address.
async fn proxy(upstream: SocketAddr, faults: Faults) -> SocketAddr {
    let l = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = l.local_addr().unwrap();
    let cfg = Config {
        upstream: upstream.to_string(),
        faults,
        seed: 1,
        verbose: false,
    };
    tokio::spawn(relay::serve(l, cfg));
    addr
}

#[tokio::test]
async fn no_faults_is_a_transparent_pipe() {
    let p = proxy(echo_server().await, Faults::default()).await;
    let mut c = TcpStream::connect(p).await.unwrap();
    c.write_all(b"hello").await.unwrap();
    let mut buf = [0u8; 5];
    c.read_exact(&mut buf).await.unwrap();
    assert_eq!(&buf, b"hello");
}

#[tokio::test]
async fn latency_is_paid_once_per_trip() {
    let p = proxy(
        echo_server().await,
        Faults {
            latency: ms(100),
            ..Default::default()
        },
    )
    .await;
    let mut c = TcpStream::connect(p).await.unwrap();
    let mut buf = [0u8; 1];

    let t = Instant::now();
    c.write_all(b"x").await.unwrap();
    c.read_exact(&mut buf).await.unwrap();
    let rtt = t.elapsed();
    assert!(rtt >= ms(200), "there and back is 2 × latency, got {rtt:?}");
    assert!(rtt < ms(600), "got {rtt:?}");
}

#[tokio::test]
async fn latency_pipelines_instead_of_adding_up() {
    // 20 separate writes at 100 ms latency. Delayed one after another they'd
    // take 20 × 2 × 100 ms = 4 s; in flight together, ~200 ms + send time.
    let p = proxy(
        echo_server().await,
        Faults {
            latency: ms(100),
            ..Default::default()
        },
    )
    .await;
    let c = TcpStream::connect(p).await.unwrap();
    c.set_nodelay(true).unwrap();
    let (mut r, mut w) = c.into_split();

    let t = Instant::now();
    let send = tokio::spawn(async move {
        for i in 0..20u8 {
            w.write_all(&[i]).await.unwrap();
            sleep(ms(5)).await;
        }
        w
    });
    let mut got = [0u8; 20];
    r.read_exact(&mut got).await.unwrap();
    let elapsed = t.elapsed();
    let _w = send.await.unwrap();

    assert_eq!(got.to_vec(), (0..20).collect::<Vec<u8>>());
    assert!(elapsed < ms(1500), "latency must overlap, took {elapsed:?}");
}

#[tokio::test]
async fn jitter_never_reorders_bytes() {
    // Jitter (±80 ms) far larger than the gap between writes (2 ms): many
    // chunks roll a shorter delay than the one before them.
    let faults = Faults {
        latency: ms(80),
        jitter: ms(80),
    };
    let p = proxy(echo_server().await, faults).await;
    let c = TcpStream::connect(p).await.unwrap();
    c.set_nodelay(true).unwrap();
    let (mut r, mut w) = c.into_split();

    tokio::spawn(async move {
        for i in 0..50u8 {
            w.write_all(&[i]).await.unwrap();
            sleep(ms(2)).await;
        }
        w
    });
    let mut got = [0u8; 50];
    r.read_exact(&mut got).await.unwrap();
    assert_eq!(got.to_vec(), (0..50).collect::<Vec<u8>>());
}

#[tokio::test]
async fn half_close_still_delivers_the_response() {
    // Send, then shut down writing (FIN). The proxy must forward the FIN
    // and keep relaying the other way until the server closes too.
    let p = proxy(
        echo_server().await,
        Faults {
            latency: ms(50),
            ..Default::default()
        },
    )
    .await;
    let mut c = TcpStream::connect(p).await.unwrap();
    let payload = vec![7u8; 100_000];
    c.write_all(&payload).await.unwrap();
    c.shutdown().await.unwrap();

    let mut got = Vec::new();
    c.read_to_end(&mut got).await.unwrap();
    assert_eq!(got.len(), payload.len());
}
