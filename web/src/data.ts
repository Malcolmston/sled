// Library content for the sled documentation site. Mirrors the shape used by
// the malcolmston/go landing site's data.ts so the sibling sites stay in sync.
export interface Lib {
  id: string; name: string; icon: string; accent: string; pkg: string; node: string;
  repo: string; docs: string; tagline: string; blurb: string; tags: string[];
  features: string[]; node_code: string; go_code: string; integrate: string;
}

export const NODE_ACCENT = '#8cc84b';

export const SLED: Lib = {
  id:"sled", name:"sled", icon:'<i class="fa-solid fa-database"></i>', accent:"#e0803c",
  pkg:"github.com/malcolmston/sled", node:"spacejam/sled",
  repo:"https://github.com/malcolmston/sled", docs:"https://malcolmston.github.io/sled/",
  tagline:"An embedded, transactional, crash-safe key/value store in pure Go.",
  blurb:"A small embedded key/value store written in pure Go with no third-party dependencies and no cgo, "+
    "inspired by the Rust sled crate. All state lives in one append-only write-ahead log: every durable "+
    "mutation — a single Set/Delete, a Batch, or a transaction — is encoded as exactly one length-prefixed, "+
    "CRC-32 checksummed record and appended to the log, so groups of writes are applied all-or-nothing on "+
    "recovery. The in-memory index is an immutable, ordered persistent treap published through a single "+
    "atomic.Pointer store, giving lock-free snapshot reads that never race the writer. On Open the log is "+
    "replayed and stops at the first torn or corrupt record, so a crash mid-write loses only the in-flight "+
    "record and the partial tail is physically truncated. On top sit atomic Batch commits, serializable "+
    "Update/View transactions, ordered prefix and bounded Scan, and a rename-atomic Compact.",
  tags:["append-only WAL","CRC-32 records","persistent treap","atomic.Pointer","crash recovery","transactions","atomic batches","ordered scan","compaction","lock-free reads","fsync durability","zero deps"],
  features:[
    "Durable append-only WAL — every commit is one length-prefixed, CRC-32 checksummed record appended via <code>DB.Set</code> / <code>DB.Delete</code>",
    "Real crash recovery — <code>Open</code> replays the log, stops at the first torn or CRC-mismatched record, and truncates the partial tail",
    "Serializable transactions — <code>DB.Update</code> commits atomically or rolls back on error/panic; <code>DB.View</code> reads a stable snapshot",
    "Atomic batches — stage many writes with <code>DB.Batch</code> / <code>NewBatch</code> and land them as one all-or-nothing durable record",
    "Ordered range scans — <code>DB.Scan</code> over a <code>Range</code> (<code>Lower</code>/<code>Upper</code>/<code>Prefix</code>) yields keys in ascending order via <code>Iterator</code>",
    "Immutable persistent-treap index published with <code>atomic.Pointer</code>, so <code>DB.Get</code> / <code>DB.Has</code> take snapshots with no locks",
    "Lock-free concurrent readers — a single writer is serialized while any number of readers proceed race-free (verified under <code>-race</code>)",
    "Compaction — <code>DB.Compact</code> rewrites the log to the live key set and installs it atomically with a rename",
    "Tunable durability — fsync-per-commit by default, or <code>WithSyncWrites(false)</code> / <code>WithFileMode</code> at <code>Open</code>",
    "Zero dependencies — pure Go standard library, no cgo, nothing to audit but the toolchain"
  ],
  node_code:
`// The original sled is a Rust embedded database crate.
use sled::Db;

let db: Db = sled::open("data.sled")?;
db.insert(b"greeting", b"hello")?;

if let Some(v) = db.get(b"greeting")? {
    println!("{}", std::str::from_utf8(&v).unwrap());
}

db.remove(b"greeting")?;
db.flush()?;`,
  go_code:
`import "github.com/malcolmston/sled"

db, _ := sled.Open("data.sled")
defer db.Close()

db.Set([]byte("greeting"), []byte("hello"))

v, ok, _ := db.Get([]byte("greeting"))
if ok {
    fmt.Printf("%s\\n", v) // hello
}

db.Delete([]byte("greeting"))`,
  integrate:
`<span class="tok-c">// Atomic read/write transaction: both writes commit together, or</span>
<span class="tok-c">// neither does. Returning an error (or panicking) rolls it all back.</span>
err := db.Update(func(tx *sled.Tx) error {
	if err := tx.Set([]byte("a"), []byte("1")); err != nil {
		return err
	}
	return tx.Set([]byte("b"), []byte("2"))
})

<span class="tok-c">// Group many writes into one all-or-nothing durable record.</span>
_ = db.Batch(func(b *sled.Batch) error {
	b.Set([]byte("user:1"), []byte("ada"))
	b.Set([]byte("user:2"), []byte("alan"))
	b.Delete([]byte("user:0"))
	return nil
})

<span class="tok-c">// Ordered prefix scan over an immutable snapshot — never races a writer.</span>
it := db.Scan(sled.Range{Prefix: []byte("user:")})
for it.Valid() {
	fmt.Printf("%s = %s\\n", it.Key(), it.Value())
	it.Next()
}

<span class="tok-c">// Half-open bounded range [b, e), read inside a snapshot transaction.</span>
_ = db.View(func(tx *sled.Tx) error {
	r := tx.Scan(sled.Range{Lower: []byte("b"), Upper: []byte("e")})
	for r.Valid() {
		r.Next()
	}
	return nil
})

<span class="tok-c">// Reclaim space: rewrite the log to just the live keys, installed atomically.</span>
if err := db.Compact(); err != nil {
	log.Fatal(err)
}`
};
