import React from 'react';
import { render } from '@testing-library/react-native';
import { Platform, Text, useWindowDimensions } from 'react-native';
import { Shell } from './Shell';

// The one branch in this file: web at a desktop width gets a constrained
// phone-width column, everything else gets the full-bleed native layout.
// PHONE_MAX is 520 and SHELL_W is 440, mirrored here rather than imported
// since they are intentionally not exported (only the behaviour is public).

jest.mock('react-native/Libraries/Utilities/useWindowDimensions');

function mockDimensions(width: number, height = 900) {
  (useWindowDimensions as jest.Mock).mockReturnValue({ width, height, scale: 2, fontScale: 1 });
}

describe('Shell', () => {
  afterEach(() => {
    Platform.OS = 'ios';
  });

  it('renders full-bleed on native, regardless of width', async () => {
    Platform.OS = 'ios';
    mockDimensions(1200);
    const { toJSON } = await render(<Shell><Text>content</Text></Shell>);
    const tree = toJSON() as any;
    // Full-bleed is a single View with no fixed device-frame width.
    expect(tree.props.style.flex).toBe(1);
  });

  it('renders full-bleed on web below the phone breakpoint', async () => {
    Platform.OS = 'web';
    mockDimensions(400);
    const { toJSON } = await render(<Shell><Text>content</Text></Shell>);
    const tree = toJSON() as any;
    expect(tree.props.style.flex).toBe(1);
  });

  it('constrains to a phone-width column on web above the breakpoint', async () => {
    Platform.OS = 'web';
    mockDimensions(1200);
    const { toJSON } = await render(<Shell><Text>content</Text></Shell>);
    const tree = toJSON() as any;
    // The outer page wrapper centres a fixed-width device frame as the
    // second level of the tree; its width is the 440px phone column.
    const device = tree.children[0];
    expect(device.props.style[1].width).toBe(440);
  });

  it('caps the device height at 940 even on a very tall desktop window', async () => {
    Platform.OS = 'web';
    mockDimensions(1200, 3000);
    const { toJSON } = await render(<Shell><Text>content</Text></Shell>);
    const tree = toJSON() as any;
    const device = tree.children[0];
    expect(device.props.style[1].height).toBe(940);
  });

  it('still renders its children', async () => {
    Platform.OS = 'web';
    mockDimensions(1200);
    const { getByText } = await render(<Shell><Text>content</Text></Shell>);
    expect(getByText('content')).toBeTruthy();
  });
});
