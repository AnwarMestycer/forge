// Tiny migration runner: applies migrations/NNN_name.up.sql in order and
// records them in schema_migrations. `node dist/migrate.js down` reverts the last one.
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { Client } from 'pg';

const dir = join(__dirname, '..', 'migrations');

async function main() {
  const url = process.env.MIGRATION_DATABASE_URL;
  if (!url) throw new Error('MIGRATION_DATABASE_URL is not set');

  const db = new Client({ connectionString: url });
  await db.connect();
  try {
    await db.query(`create table if not exists schema_migrations (
      version text primary key,
      applied_at timestamptz not null default now()
    )`);
    const { rows } = await db.query<{ version: string }>('select version from schema_migrations order by version');
    const applied = new Set(rows.map((r) => r.version));

    if (process.argv[2] === 'down') {
      const last = rows.at(-1)?.version;
      if (!last) return console.log('migrate: nothing to revert');
      await inTx(db, readFileSync(join(dir, `${last}.down.sql`), 'utf8'), 'delete from schema_migrations where version = $1', last);
      return console.log(`migrate: reverted ${last}`);
    }

    const pending = readdirSync(dir)
      .filter((f) => f.endsWith('.up.sql'))
      .map((f) => f.replace(/\.up\.sql$/, ''))
      .filter((v) => !applied.has(v))
      .sort();
    for (const version of pending) {
      await inTx(db, readFileSync(join(dir, `${version}.up.sql`), 'utf8'), 'insert into schema_migrations (version) values ($1)', version);
      console.log(`migrate: applied ${version}`);
    }
    if (pending.length === 0) console.log('migrate: up to date');
  } finally {
    await db.end();
  }
}

async function inTx(db: Client, sql: string, record: string, version: string) {
  await db.query('begin');
  try {
    await db.query(sql);
    await db.query(record, [version]);
    await db.query('commit');
  } catch (e) {
    await db.query('rollback');
    throw e;
  }
}

main().catch((e) => {
  console.error('migrate:', e.message);
  process.exit(1);
});
