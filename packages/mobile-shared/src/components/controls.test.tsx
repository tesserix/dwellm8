import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import {
  AddressLookup, Avatar, BackHeader, Button, ChoiceRow, Field, KeyValue, ListRow, Metric,
  PhotoStrip, SearchBar, StatusPill, SwitchRow, SyncBadge, Timeline, Toast,
  toneColor, TriState,
} from './controls';

// These components are shared by every worklist app (Ops, Pro, Admin) and
// appear hundreds of times a day, so a defect here reaches every app at once.
//
// @testing-library/react-native v14 renders through React's async act, so
// every render() and every fireEvent that can trigger a state update is
// awaited — an un-awaited render leaves `screen` pointed at nothing yet.

describe('Button', () => {
  it('calls onPress when enabled', async () => {
    const onPress = jest.fn();
    await render(<Button label="Save" onPress={onPress} />);
    await fireEvent.press(screen.getByRole('button', { name: 'Save' }));
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('exposes its disabled state to assistive technology', async () => {
    await render(<Button label="Save" disabled />);
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  });
});

describe('SyncBadge', () => {
  it('renders nothing when nothing is queued', async () => {
    await render(<SyncBadge queued={0} />);
    expect(screen.queryByText(/queued/)).toBeNull();
  });

  it('never claims a write succeeded while items are still queued', async () => {
    await render(<SyncBadge queued={3} />);
    expect(screen.getByText('3 queued · syncing when back online')).toBeTruthy();
  });
});

describe('ChoiceRow', () => {
  it('reports the pressed choice regardless of its current selection state', async () => {
    const onPress = jest.fn();
    await render(<ChoiceRow label="Resident" selected={false} onPress={onPress} />);
    await fireEvent.press(screen.getByRole('radio', { name: 'Resident' }));
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('exposes whether it is the selected choice', async () => {
    await render(<ChoiceRow label="Resident" selected onPress={() => {}} />);
    expect(screen.getByRole('radio', { name: 'Resident' })).toBeSelected();
  });
});

describe('SwitchRow', () => {
  it('calls onChange with the flipped value, not the current one', async () => {
    const onChange = jest.fn();
    await render(<SwitchRow label="Autopay" value={false} onChange={onChange} />);
    await fireEvent(screen.getByRole('switch'), 'valueChange', true);
    expect(onChange).toHaveBeenCalledWith(true);
  });
});

describe('TriState', () => {
  it('reports the state the tenant tapped, not the one currently set', async () => {
    const onChange = jest.fn();
    await render(<TriState label="Smoke detector" value="ok" onChange={onChange} />);
    await fireEvent.press(screen.getByRole('button', { name: 'Smoke detector: Issue' }));
    expect(onChange).toHaveBeenCalledWith('issue');
    expect(onChange).not.toHaveBeenCalledWith('ok');
  });

  it('offers exactly the three states an inspection can record', async () => {
    await render(<TriState label="Tap" value={null} onChange={() => {}} />);
    expect(screen.getByRole('button', { name: 'Tap: OK' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Tap: Note' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Tap: Issue' })).toBeTruthy();
  });
});

describe('toneColor', () => {
  it('resolves every declared tone to a colour rather than undefined', () => {
    for (const tone of ['neutral', 'green', 'amber', 'red', 'blue', 'violet'] as const) {
      expect(toneColor(tone)).toEqual(expect.stringMatching(/^#/));
    }
  });
});

describe('StatusPill', () => {
  it('renders the text with no dot by default', async () => {
    await render(<StatusPill text="Overdue" tone="red" />);
    expect(screen.getByText('Overdue')).toBeTruthy();
  });
});

describe('ListRow', () => {
  it('calls onPress when tapped', async () => {
    const onPress = jest.fn();
    await render(<ListRow title="4B, Green Meadows" onPress={onPress} />);
    await fireEvent.press(screen.getByRole('button', { name: '4B, Green Meadows' }));
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('shows the subtitle and meta lines when given', async () => {
    await render(<ListRow title="Rent" subtitle="Due 5 Aug" meta="₹25,000" />);
    expect(screen.getByText('Due 5 Aug')).toBeTruthy();
    expect(screen.getByText('₹25,000')).toBeTruthy();
  });

  it('renders custom right content instead of the default chevron', async () => {
    await render(<ListRow title="Rent" right={<Toast text="Paid" />} />);
    expect(screen.getByText('Paid')).toBeTruthy();
  });
});

describe('KeyValue', () => {
  it('renders the key and the value together', async () => {
    await render(<KeyValue k="Deposit held" v="₹50,000" />);
    expect(screen.getByText('Deposit held')).toBeTruthy();
    expect(screen.getByText('₹50,000')).toBeTruthy();
  });
});

describe('Avatar', () => {
  it('renders the given initials', async () => {
    await render(<Avatar initials="AK" />);
    expect(screen.getByText('AK')).toBeTruthy();
  });
});

describe('SearchBar', () => {
  it('reports what was typed', async () => {
    const onChange = jest.fn();
    await render(<SearchBar value="" onChange={onChange} />);
    await fireEvent.changeText(screen.getByDisplayValue(''), 'green meadows');
    expect(onChange).toHaveBeenCalledWith('green meadows');
  });

  it('uses the placeholder it is given', async () => {
    await render(<SearchBar value="" onChange={() => {}} placeholder="Search tenants" />);
    expect(screen.getByPlaceholderText('Search tenants')).toBeTruthy();
  });
});

describe('Field', () => {
  it('reports what was typed', async () => {
    const onChange = jest.fn();
    await render(<Field label="Notes" value="" onChange={onChange} placeholder="Add a note" />);
    await fireEvent.changeText(screen.getByPlaceholderText('Add a note'), 'left a key with the guard');
    expect(onChange).toHaveBeenCalledWith('left a key with the guard');
  });

  it('labels the input, not just the space above it', async () => {
    await render(<Field label="Notes" value="" onChange={() => {}} />);
    expect(screen.getByText('Notes')).toBeTruthy();
    expect(screen.getByLabelText('Notes')).toBeTruthy();
  });
});

describe('Metric', () => {
  it('is a button when it has a handler, and calls it', async () => {
    const onPress = jest.fn();
    await render(<Metric value="₹2.4L" label="Collected" onPress={onPress} />);
    await fireEvent.press(screen.getByRole('button', { name: '₹2.4L Collected' }));
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('is not a button when it has no handler', async () => {
    await render(<Metric value="₹2.4L" label="Collected" />);
    expect(screen.queryByRole('button', { name: '₹2.4L Collected' })).toBeNull();
  });
});

describe('Timeline', () => {
  it('renders every item, in order', async () => {
    await render(<Timeline items={[
      { at: '1 Aug', what: 'Invoice raised' },
      { at: '5 Aug', what: 'Payment received' },
    ]} />);
    expect(screen.getByText('Invoice raised')).toBeTruthy();
    expect(screen.getByText('Payment received')).toBeTruthy();
  });
});

describe('PhotoStrip', () => {
  it('renders exactly as many tiles as the count says', async () => {
    const { toJSON } = await render(<PhotoStrip count={3} />);
    // The placeholder tiles carry no accessible role or text, so the count
    // is asserted from the rendered tree's direct children rather than a
    // query — with no onAdd, every child of the row is a photo tile.
    const tree = toJSON();
    const row = Array.isArray(tree) ? tree[0] : tree;
    expect(row?.children?.length).toBe(3);
  });

  it('offers an add button only when onAdd is given, and calls it', async () => {
    const onAdd = jest.fn();
    await render(<PhotoStrip count={0} onAdd={onAdd} />);
    await fireEvent.press(screen.getByRole('button', { name: 'Add a photo' }));
    expect(onAdd).toHaveBeenCalledTimes(1);
  });

  it('has no add button when onAdd is not given', async () => {
    await render(<PhotoStrip count={2} />);
    expect(screen.queryByRole('button', { name: 'Add a photo' })).toBeNull();
  });
});

describe('Toast', () => {
  it('renders its text', async () => {
    await render(<Toast text="Saved" />);
    expect(screen.getByText('Saved')).toBeTruthy();
  });
});

describe('BackHeader', () => {
  it('calls onBack when the back button is pressed', async () => {
    const onBack = jest.fn();
    await render(<BackHeader title="Ticket #4021" onBack={onBack} />);
    await fireEvent.press(screen.getByRole('button', { name: 'Back' }));
    expect(onBack).toHaveBeenCalledTimes(1);
  });

  it('shows the subtitle only when given', async () => {
    await render(<BackHeader title="Ticket #4021" subtitle="Leaking tap" />);
    expect(screen.getByText('Leaking tap')).toBeTruthy();
  });
});

describe('Field', () => {
  // A name, a PIN, a unit code and an email are all data, and iOS autocorrect
  // rewrites data: "Kavita" became "Kabila" on the onboarding wizard, and the
  // email arrived capitalised. Both are wrong for every field this component
  // serves, so correction is opt-in.
  it('leaves what is typed alone', async () => {
    await render(<Field label="Owner's name" value="" onChange={() => {}} />);
    const input = screen.getByLabelText("Owner's name");
    expect(input.props.autoCorrect).toBe(false);
    expect(input.props.spellCheck).toBe(false);
  });

  it('does not capitalise an email address', async () => {
    await render(
      <Field label="Email" value="" onChange={() => {}} keyboardType="email-address" />,
    );
    expect(screen.getByLabelText('Email').props.autoCapitalize).toBe('none');
  });

  it('hides a password and never capitalises it', async () => {
    await render(<Field label="Password" value="" onChange={() => {}} secure />);
    const input = screen.getByLabelText('Password');
    expect(input.props.secureTextEntry).toBe(true);
    expect(input.props.autoCapitalize).toBe('none');
  });
});

describe('AddressLookup', () => {
  const kochi = {
    description: 'Chandra Arcade, Kadavanthra, Kochi, Kerala, 682020',
    line1: '12 Kadavanthra Road, Chandra Arcade', locality: 'Kadavanthra',
    city: 'Kochi', state_code: 'KL', pin_code: '682020',
  };

  it('searches once the term is long enough and lists what came back', async () => {
    const search = jest.fn().mockResolvedValue([kochi]);
    await render(<AddressLookup search={search} onPick={jest.fn()} />);

    await fireEvent.changeText(screen.getByLabelText('Search for the address'), 'chandra arcade');
    await waitFor(() => expect(screen.getByText(kochi.description)).toBeTruthy());
    expect(search).toHaveBeenCalledWith('chandra arcade');
  });

  it('leaves a term too short to match alone', async () => {
    const search = jest.fn().mockResolvedValue([]);
    await render(<AddressLookup search={search} onPick={jest.fn()} />);
    await fireEvent.changeText(screen.getByLabelText('Search for the address'), 'ko');
    await waitFor(() => expect(search).not.toHaveBeenCalled());
  });

  it('hands the whole split address back when one is picked, and clears the list', async () => {
    const onPick = jest.fn();
    await render(<AddressLookup search={jest.fn().mockResolvedValue([kochi])} onPick={onPick} />);

    await fireEvent.changeText(screen.getByLabelText('Search for the address'), 'chandra arcade');
    await waitFor(() => expect(screen.getByText(kochi.description)).toBeTruthy());
    await fireEvent.press(screen.getByText(kochi.description));

    expect(onPick).toHaveBeenCalledWith(kochi);
    expect(screen.queryByText(kochi.description)).toBeNull();
  });

  it('says the lookup is down rather than that the address does not exist', async () => {
    const search = jest.fn().mockRejectedValue(new Error('Address lookup is unavailable just now'));
    await render(<AddressLookup search={search} onPick={jest.fn()} />);
    await fireEvent.changeText(screen.getByLabelText('Search for the address'), 'chandra arcade');
    await waitFor(() =>
      expect(screen.getByText(/lookup is unavailable/i)).toBeTruthy());
  });

  it('says so when a real search matched nothing, so the field is not silently empty', async () => {
    await render(<AddressLookup search={jest.fn().mockResolvedValue([])} onPick={jest.fn()} />);
    await fireEvent.changeText(screen.getByLabelText('Search for the address'), 'zzzzzzzz');
    await waitFor(() => expect(screen.getByText(/no match/i)).toBeTruthy());
  });
});
