import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { DocsView } from '../../../src/components/DocsView';
import type { DocIndex } from 'go-ui';

// A minimal DocIndex the stubbed fetch returns for DocsApp's doc.json request.
const DOC_INDEX: DocIndex = {
  module: 'github.com/malcolmston/sled',
  packages: [
    {
      importPath: 'github.com/malcolmston/sled',
      name: 'sled',
      synopsis: 'Package sled is a small, embedded, transactional, crash-safe key/value store in pure Go.',
      doc: 'Package sled is a small, embedded, transactional, crash-safe key/value store in pure Go.',
      consts: [],
      vars: [],
      types: [
        {
          name: 'DB',
          signature: 'type DB struct{}',
          doc: 'DB is an embedded, transactional key/value store backed by an append-only write-ahead log.',
          consts: [],
          vars: [],
          funcs: [],
          methods: [],
        },
      ],
      funcs: [{ name: 'Open', signature: 'func Open(path string, opts ...Option) (*DB, error)', doc: 'Open opens (creating it if necessary) the database whose write-ahead log lives at path.' }],
    },
  ],
};

describe('DocsView', () => {
  beforeEach(() => {
    // DocsApp fetches doc.json; return the small index.
    global.fetch = vi.fn((input: RequestInfo | URL) => {
      if (String(input).includes('doc.json')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(DOC_INDEX) } as Response);
      }
      return new Promise<Response>(() => {});
    }) as unknown as typeof fetch;
  });

  it('renders the inline React API reference from the fetched doc.json', async () => {
    const { container } = render(<DocsView />);
    expect(container.querySelector('#view-docs')).not.toBeNull();
    expect(
      screen.getByRole('heading', { level: 2, name: /API documentation/ }),
    ).toBeInTheDocument();

    // DocsApp fetches asynchronously, then renders the package view + symbols.
    expect(await screen.findByRole('heading', { name: /package sled/ })).toBeInTheDocument();
    expect(container.querySelector('#sym-Open'), 'func Open symbol card').not.toBeNull();
    expect(container.querySelector('#sym-DB'), 'type DB symbol card').not.toBeNull();

    // The secondary link to the raw generated static HTML remains.
    expect(screen.getByRole('link', { name: /Open the raw generated HTML/ })).toHaveAttribute('href', './api/');
  });
});
