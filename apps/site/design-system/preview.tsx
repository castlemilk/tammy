import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { SiteBrand } from '../components/site-brand';
import { AccountingStatus } from '../components/accounting-status';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { Select } from '../components/ui/select';
import { SegmentedControl } from '../components/gamma/segmented-control';
import { GammaEmptyState } from '../components/gamma/empty-state';
import './preview.css';

function DesignPreview() {
  const [quarter, setQuarter] = useState('1');
  const [pending, setPending] = useState(false);
  return (
    <main className="tools-shell design-preview">
      <header>
        <SiteBrand />
        <span>Component reference · synthetic examples</span>
      </header>
      <h1>Soft Precision, in practice.</h1>
      <p className="design-preview-intro">
        A working reference for the controls, states and materials used across
        Tammy’s public site. These examples do not create financial records or
        download a PDF.
      </p>
      <div className="design-preview-grid">
        <section>
          <h2>Actions</h2>
          <p>One hierarchy, with focus, disabled and pending states.</p>
          <div className="design-preview-row">
            <Button loading={pending} onClick={() => setPending(true)}>
              Download PDF
            </Button>
            <Button variant="outline" onClick={() => setPending(false)}>
              Reset example
            </Button>
          </div>
          <div className="design-preview-row">
            <Button disabled>Unavailable</Button>
            <Button variant="ghost">Supporting action</Button>
          </div>
          <output>
            {pending
              ? 'Pending example. Use Reset example to return.'
              : 'Choose Download PDF to preview the pending state.'}
          </output>
        </section>
        <section>
          <h2>Fields</h2>
          <Label htmlFor="sample-amount">Amount in AUD</Label>
          <Input id="sample-amount" inputMode="decimal" defaultValue="100.00" />
          <Label htmlFor="sample-error">Invalid example</Label>
          <Input
            id="sample-error"
            defaultValue="100.001"
            aria-invalid="true"
            aria-describedby="sample-error-message"
          />
          <p id="sample-error-message" className="design-preview-error">
            Enter an AUD amount with up to two decimal places.
          </p>
          <Label htmlFor="sample-year">Financial year</Label>
          <Select id="sample-year" defaultValue="2026">
            <option value="2026">2025–26</option>
            <option value="2027">2026–27</option>
          </Select>
        </section>
        <section>
          <h2>Choice</h2>
          <p>
            The existing Gamma radio group. Select with pointer or arrow keys.
          </p>
          <SegmentedControl
            label="Example quarter"
            value={quarter}
            onChange={setQuarter}
            options={[1, 2, 3, 4].map((value) => ({
              value: String(value),
              label: `Q${value}`,
            }))}
            block
          />
          <p>Selected quarter: Q{quarter}</p>
          <h2 className="design-preview-subhead">Record states</h2>
          <p>Labels retain their distinct accounting meanings.</p>
          <div className="design-preview-row">
            {(
              ['draft', 'reviewed', 'posted', 'unmatched', 'issued'] as const
            ).map((status) => (
              <AccountingStatus key={status} status={status} />
            ))}
          </div>
        </section>
        <section>
          <h2>Empty results</h2>
          <GammaEmptyState
            title="No matching tools."
            description="Try a different search, or reset the filters to see all twelve tools."
          >
            <Button variant="outline">Show all tools</Button>
          </GammaEmptyState>
        </section>
      </div>
      <section className="design-preview-palette">
        <h2>One palette, distinct roles</h2>
        <div>
          {['canvas', 'paper', 'forest', 'ink', 'muted', 'sage', 'warning'].map(
            (name) => (
              <figure key={name}>
                <span style={{ background: `var(--tammy-${name})` }} />
                <figcaption>{name}</figcaption>
              </figure>
            ),
          )}
        </div>
      </section>
      <footer>
        Source: apps/site/app/tokens.css · Native selects and Gamma radios
        retain their platform keyboard behaviour. This reference is generated
        locally and has no public site route.
      </footer>
    </main>
  );
}

createRoot(document.getElementById('root')!).render(<DesignPreview />);
