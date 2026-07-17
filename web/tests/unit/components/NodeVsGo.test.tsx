import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { NodeVsGo } from '../../../src/components/NodeVsGo';
import { SLED } from '../../../src/data';

describe('NodeVsGo', () => {
  it('renders the comparison heading and both Rust and Go columns', () => {
    const { container } = render(<NodeVsGo lib={SLED} />);
    expect(container.querySelector(`#${SLED.id}-cmp`)).not.toBeNull();
    expect(screen.getByText('Rust')).toBeInTheDocument();
    expect(screen.getByText('Go')).toBeInTheDocument();
    expect(container.querySelectorAll('.compare .code').length).toBe(2);
  });
});
