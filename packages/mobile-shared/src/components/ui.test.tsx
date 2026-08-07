import React from 'react';
import { Text } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';
import {
  ActivityRow, AppHeader, AvatarButton, Badge, Banner, ChipRow, CollapsibleHeader,
  EmptyState, ErrorState, LinkRow, MoneyRow, ProgressBar, Segmented,
} from './ui';

describe('AppHeader', () => {
  it('calls onTitlePress when a handler is given', async () => {
    const onTitlePress = jest.fn();
    await render(<AppHeader title="Dashboard" onTitlePress={onTitlePress} />);
    await fireEvent.press(screen.getByText('Dashboard'));
    expect(onTitlePress).toHaveBeenCalledTimes(1);
  });

  it('is not pressable when no handler is given, so a static title cannot be tapped into nothing', async () => {
    await render(<AppHeader title="Dashboard" />);
    // Pressable disables itself (props.disabled) when there is nothing to do,
    // which toBeDisabled() checks by walking to the nearest host ancestor.
    expect(screen.getByText('Dashboard')).toBeDisabled();
  });
});

describe('AvatarButton', () => {
  it('calls onPress', async () => {
    const onPress = jest.fn();
    await render(<AvatarButton onPress={onPress} />);
    await fireEvent.press(screen.getByRole('button', { name: 'Your profile' }));
    expect(onPress).toHaveBeenCalledTimes(1);
  });
});

describe('CollapsibleHeader', () => {
  it('calls onToggle when pressed', async () => {
    const onToggle = jest.fn();
    await render(<CollapsibleHeader title="Payments" onToggle={onToggle} />);
    await fireEvent.press(screen.getByRole('button', { name: 'Payments' }));
    expect(onToggle).toHaveBeenCalledTimes(1);
  });
});

describe('Segmented', () => {
  it('reports the item tapped, and marks it selected', async () => {
    const onChange = jest.fn();
    await render(<Segmented items={['This month', 'Last month']} value="This month" onChange={onChange} />);
    await fireEvent.press(screen.getByRole('tab', { name: 'Last month' }));
    expect(onChange).toHaveBeenCalledWith('Last month');
    expect(screen.getByRole('tab', { name: 'This month' })).toBeSelected();
    expect(screen.getByRole('tab', { name: 'Last month' })).not.toBeSelected();
  });
});

describe('ChipRow', () => {
  it('reports the chip tapped, and marks it selected', async () => {
    const onChange = jest.fn();
    await render(<ChipRow
      items={[{ label: 'Overdue' }, { label: 'Upcoming' }]}
      value="Overdue"
      onChange={onChange}
    />);
    await fireEvent.press(screen.getByRole('button', { name: 'Upcoming' }));
    expect(onChange).toHaveBeenCalledWith('Upcoming');
    expect(screen.getByRole('button', { name: 'Overdue' })).toBeSelected();
  });
});

describe('MoneyRow', () => {
  it('renders the label and the value', async () => {
    await render(<MoneyRow label="Rent" value="₹25,000.00" />);
    expect(screen.getByText('Rent')).toBeTruthy();
    expect(screen.getByText('₹25,000.00')).toBeTruthy();
  });
});

describe('ProgressBar', () => {
  // The fill width is the one piece of real arithmetic in this file: it is
  // clamped so a percentage outside 0–100 cannot overflow or vanish the bar.
  it('clamps a percentage above 100 down to 100', async () => {
    const { toJSON } = await render(<ProgressBar pct={140} tint="#000" />);
    const tree = toJSON() as any;
    const fill = tree.children[0];
    expect(fill.props.style[1].width).toBe('100%');
  });

  it('clamps a percentage below the visible floor up to 2, so a real but tiny amount still shows', async () => {
    const { toJSON } = await render(<ProgressBar pct={0} tint="#000" />);
    const tree = toJSON() as any;
    const fill = tree.children[0];
    expect(fill.props.style[1].width).toBe('2%');
  });

  it('passes an ordinary percentage through unchanged', async () => {
    const { toJSON } = await render(<ProgressBar pct={42} tint="#000" />);
    const tree = toJSON() as any;
    const fill = tree.children[0];
    expect(fill.props.style[1].width).toBe('42%');
  });
});

describe('EmptyState', () => {
  it('renders the title and body', async () => {
    await render(<EmptyState title="No tickets yet" body="Everything is quiet." />);
    expect(screen.getByText('No tickets yet')).toBeTruthy();
    expect(screen.getByText('Everything is quiet.')).toBeTruthy();
  });
});

describe('ErrorState', () => {
  it('says what went wrong and offers a retry (#343)', async () => {
    const retry = jest.fn();
    await render(<ErrorState error="This is not available on this server yet." onRetry={retry} />);
    expect(screen.getByText('This is not available on this server yet.')).toBeTruthy();
    await fireEvent.press(screen.getByText('Try again'));
    expect(retry).toHaveBeenCalled();
  });

  it('leaves the retry out when there is nothing to retry', async () => {
    await render(<ErrorState error="The API is not configured on this build." />);
    expect(screen.queryByText('Try again')).toBeNull();
  });
});

describe('Banner', () => {
  it('renders its children', async () => {
    await render(<Banner><Text>Rent is overdue</Text></Banner>);
    expect(screen.getByText('Rent is overdue')).toBeTruthy();
  });

  // onClose is accepted but never wired to a dismiss control or called from
  // anywhere in the component — a caller passing it today gets a banner that
  // cannot be dismissed. Documented so a caller does not rely on it, and so
  // fixing it is a visible test change rather than a silent behaviour change.
  it('does not yet render any way to call onClose', async () => {
    const onClose = jest.fn();
    await render(<Banner onClose={onClose}><Text>Notice</Text></Banner>);
    expect(screen.queryByRole('button')).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe('Badge', () => {
  it('renders its text', async () => {
    await render(<Badge text="New" />);
    expect(screen.getByText('New')).toBeTruthy();
  });
});

describe('LinkRow', () => {
  it('calls onPress', async () => {
    const onPress = jest.fn();
    await render(<LinkRow label="Agreement" onPress={onPress} />);
    await fireEvent.press(screen.getByRole('button', { name: 'Agreement' }));
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('shows the value line only when given', async () => {
    await render(<LinkRow label="PAN" value="ABCDE1234F" />);
    expect(screen.getByText('ABCDE1234F')).toBeTruthy();
  });
});

describe('ActivityRow', () => {
  it('calls onPress', async () => {
    const onPress = jest.fn();
    await render(<ActivityRow icon={null} title="Rent paid" onPress={onPress} />);
    await fireEvent.press(screen.getByRole('button', { name: 'Rent paid' }));
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('shows the meta text only when given', async () => {
    await render(<ActivityRow icon={null} title="Rent paid" meta="2 Aug" />);
    expect(screen.getByText('2 Aug')).toBeTruthy();
  });
});
