import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { metadata } from '../app/layout';
import Home from '../app/page';

afterEach(cleanup);

describe('Tammy public routes', () => {
  it('states the supported platform and preparation-only boundary', () => {
    render(<Home />);

    expect(
      screen.getByRole('heading', { name: 'Local accounting for Australia' }),
    ).toBeTruthy();
    expect(screen.getByText(/macOS 14 or later.*Apple silicon/i)).toBeTruthy();
    expect(screen.getByText(/preparation-only.*not lodged/i)).toBeTruthy();
    expect(screen.getByText('Gamma Systems Pty Ltd')).toBeTruthy();
    expect(
      screen.getByText(
        /encrypted workspace.*journals.*source documents.*bank transactions.*GST workpaper/i,
      ),
    ).toBeTruthy();
    expect(screen.queryByText(/company (?:EOFY|tax return)/i)).toBeNull();
    expect(String(metadata.description)).not.toMatch(/company\s+EOFY/i);
    expect(String(metadata.description)).not.toMatch(/tax return preparation/i);
  });
});
