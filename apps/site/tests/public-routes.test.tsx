import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import Home from '../app/page';

describe('Tammy public routes', () => {
  it('states the supported platform and preparation-only boundary', () => {
    render(<Home />);

    expect(
      screen.getByRole('heading', { name: 'Local accounting for Australia' }),
    ).toBeTruthy();
    expect(screen.getByText(/macOS 14 or later.*Apple silicon/i)).toBeTruthy();
    expect(screen.getByText(/preparation-only.*not lodged/i)).toBeTruthy();
    expect(screen.getByText('Gamma Systems Pty Ltd')).toBeTruthy();
  });
});
