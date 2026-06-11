import type {ReactNode} from 'react'
import clsx from 'clsx'
import Link from '@docusaurus/Link'
import useBaseUrl from '@docusaurus/useBaseUrl'
import useDocusaurusContext from '@docusaurus/useDocusaurusContext'
import Layout from '@theme/Layout'
import Heading from '@theme/Heading'
import CodeBlock from '@theme/CodeBlock'
import styles from './index.module.css'

function Browser({src, alt}: {src: string; alt: string}): ReactNode {
  return (
    <div className={styles.browser}>
      <div className={styles.browserBar}>
        <span /><span /><span />
        <div className={styles.browserUrl}>app.teggo.example</div>
      </div>
      <img className={styles.browserImg} src={useBaseUrl(src)} alt={alt} loading="lazy" />
    </div>
  )
}

function useAdminUrl(): string {
  const {siteConfig} = useDocusaurusContext()
  return (siteConfig.customFields?.adminUrl as string) ?? '/getting-started'
}

function Hero(): ReactNode {
  const adminUrl = useAdminUrl()
  return (
    <header className={styles.hero}>
      <div className={clsx('container', styles.heroInner)}>
        <span className={styles.eyebrow}>Self-hosted B2B commerce platform</span>
        <Heading as="h1" className={styles.heroTitle}>
          The commerce platform your buyers and your team actually want.
        </Heading>
        <p className={styles.heroSubtitle}>
          Teggo unifies commerce, CRM and workflow automation for manufacturers,
          distributors and wholesalers — on infrastructure you own. One Go service,
          one API contract, a no-code storefront builder and an AI assistant built in.
        </p>
        <div className={styles.ctaRow}>
          <Link className="button button--primary button--lg" href={adminUrl}>
            Get started
          </Link>
          <Link className="button button--secondary button--lg" to="/intro">
            Explore the platform
          </Link>
        </div>
        <Browser src="/img/shot-builder.png" alt="Teggo storefront page builder" />
      </div>
    </header>
  )
}

function TrustStrip(): ReactNode {
  const items = ['Manufacturers', 'Distributors', 'Wholesalers', 'Marketplaces']
  return (
    <section className={styles.trust}>
      <div className={clsx('container', styles.trustInner)}>
        <span className={styles.trustLabel}>Built for B2B</span>
        <ul className={styles.trustList}>
          {items.map((i) => (
            <li key={i}>{i}</li>
          ))}
        </ul>
      </div>
    </section>
  )
}

type Row = {
  eyebrow: string
  title: string
  body: string
  points: string[]
  cta: {label: string; to: string}
  reversed?: boolean
  visual: ReactNode
}

function FeatureRow({row}: {row: Row}): ReactNode {
  return (
    <section className={styles.row}>
      <div className={clsx('container', styles.rowInner, row.reversed && styles.rowReversed)}>
        <div className={styles.rowCopy}>
          <span className={styles.rowEyebrow}>{row.eyebrow}</span>
          <Heading as="h2" className={styles.rowTitle}>{row.title}</Heading>
          <p className={styles.rowBody}>{row.body}</p>
          <ul className={styles.checkList}>
            {row.points.map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
          <Link className={clsx('button button--primary', styles.rowCta)} to={row.cta.to}>
            {row.cta.label}
          </Link>
        </div>
        <div className={styles.rowVisual}>{row.visual}</div>
      </div>
    </section>
  )
}

const ROWS: Row[] = [
  {
    eyebrow: 'Storefront',
    title: 'Launch storefronts without writing code',
    body: 'Compose pages from blocks on a drag-and-drop canvas with a live, true-to-production preview — or describe the page and let AI draft it for you to refine and publish.',
    points: [
      'Hero, banner, rich-text and product-grid blocks',
      'AI page generation from a plain-language brief',
      'WYSIWYG preview powered by the real renderer',
    ],
    cta: {label: 'See the frontend guide', to: '/frontend'},
    visual: <Browser src="/img/shot-storefront.png" alt="Drag-and-drop storefront builder" />,
  },
  {
    eyebrow: 'Automation',
    title: 'Automate operations on a visual canvas',
    body: 'Design rules as a flow: when an event fires and your conditions match, actions run as background jobs on a Postgres-backed queue — no scripts, no cron sprawl.',
    points: [
      'Trigger → conditions → actions, drawn as a graph',
      'Order, quote and schedule-driven triggers',
      'Reliable execution via the river job queue',
    ],
    cta: {label: 'Read about background jobs', to: '/background-jobs'},
    reversed: true,
    visual: <Browser src="/img/shot-automation.png" alt="Visual automation flow canvas" />,
  },
  {
    eyebrow: 'Developer-first',
    title: 'One API contract, every surface',
    body: 'A single OpenAPI 3.1 document is the source of truth. The typed client, the Vue admin, the Nuxt storefront and this documentation are all generated from it — so nothing drifts.',
    points: [
      'OpenAPI 3.1 → generated TypeScript client',
      'End-to-end type safety across frontends',
      'Auto-generated, always-current API reference',
    ],
    cta: {label: 'Browse the API reference', to: '/api'},
    visual: (
      <CodeBlock language="ts" title="Typed, generated from the contract">{`import { api } from '@teggo/api'

// Fully typed against the OpenAPI 3.1 contract
const { data } = await api.GET('/admin/products', {
  params: { query: { q: 'valve', page: 1 } },
})

data.items.forEach((p) => console.log(p.sku, p.name))`}</CodeBlock>
    ),
  },
]

type Cap = {icon: string; title: string; body: string}
const CAPS: Cap[] = [
  {icon: '🛒', title: 'Catalog & pricing', body: 'Products, categories, attributes, customer-group price lists and quotes.'},
  {icon: '🧾', title: 'Order-to-cash', body: 'Carts, orders, approvals, invoicing and payments — end to end.'},
  {icon: '🤝', title: 'CRM & accounts', body: 'Companies, buyers, roles, credit limits and account health.'},
  {icon: '📦', title: 'Inventory', body: 'Multi-warehouse stock, availability-to-promise and reorder signals.'},
  {icon: '🏬', title: 'Marketplace', body: 'Vendor onboarding, product approvals, orders and payouts.'},
  {icon: '🤖', title: 'AI assistant', body: 'Answers across orders, catalog, customers and stock — under your permissions.'},
]

function Capabilities(): ReactNode {
  return (
    <section className={styles.caps}>
      <div className="container">
        <div className={styles.sectionHead}>
          <Heading as="h2" className={styles.sectionTitle}>The whole platform, not a stack of integrations</Heading>
          <p className={styles.sectionLede}>Commerce, CRM and a low-code workflow engine in one Go service on PostgreSQL.</p>
        </div>
        <div className={styles.capGrid}>
          {CAPS.map((c) => (
            <div key={c.title} className={styles.capCard}>
              <div className={styles.capIcon} aria-hidden="true">{c.icon}</div>
              <Heading as="h3" className={styles.capTitle}>{c.title}</Heading>
              <p className={styles.capBody}>{c.body}</p>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}

const STATS = [
  {k: '1', v: 'OpenAPI contract — zero client drift'},
  {k: '3', v: 'frontends from one typed client'},
  {k: '100%', v: 'self-hosted, your infrastructure'},
  {k: 'Postgres', v: 'data, jobs & search in one DB'},
]

function Stats(): ReactNode {
  return (
    <section className={styles.stats}>
      <div className={clsx('container', styles.statsGrid)}>
        {STATS.map((s) => (
          <div key={s.v} className={styles.stat}>
            <div className={styles.statK}>{s.k}</div>
            <div className={styles.statV}>{s.v}</div>
          </div>
        ))}
      </div>
    </section>
  )
}

function FinalCta(): ReactNode {
  const adminUrl = useAdminUrl()
  return (
    <section className={styles.finalCta}>
      <div className={clsx('container', styles.finalInner)}>
        <Heading as="h2" className={styles.finalTitle}>Run the whole stack locally in minutes.</Heading>
        <p className={styles.finalLede}>Clone, bring it up with Docker Compose, and start building.</p>
        <div className={styles.terminal} aria-label="Quickstart">
          <div className={styles.terminalBar}><span /><span /><span /></div>
          <pre className={styles.terminalBody}><code>
            <span className={styles.prompt}>$</span> git clone https://github.com/teggo/teggo{'\n'}
            <span className={styles.prompt}>$</span> make up   <span className={styles.comment}># Postgres + API + admin + storefront</span>{'\n'}
            <span className={styles.prompt}>$</span> open http://localhost:8080
          </code></pre>
        </div>
        <div className={styles.ctaRow}>
          <Link className="button button--primary button--lg" href={adminUrl}>
            Get started
          </Link>
          <Link className="button button--outline button--lg" to="/architecture">
            How it works
          </Link>
        </div>
      </div>
    </section>
  )
}

export default function Home(): ReactNode {
  const {siteConfig} = useDocusaurusContext()
  return (
    <Layout title="Teggo — self-hosted B2B commerce" description={siteConfig.tagline}>
      <Hero />
      <main>
        <TrustStrip />
        {ROWS.map((r) => (
          <FeatureRow key={r.title} row={r} />
        ))}
        <Capabilities />
        <Stats />
        <FinalCta />
      </main>
    </Layout>
  )
}
