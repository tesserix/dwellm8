import React, { useMemo, useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Button, ActionBar, ChoiceRow, Field,
  Toast, KeyValue,
  color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import { useOpsTicket } from '../src/data/worklists';
import { nextSlots, useDispatch } from '../src/data/dispatch';

/**
 * Dispatch — name the vendor and pick a slot.
 *
 * There is no vendor panel to rank yet, so the manager types who they are
 * sending. The slot is scheduled on the ticket, which is what the tenant sees.
 */

export default function Dispatch() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { ticket } = useLocalSearchParams<{ ticket?: string }>();
  const { loading, error, data: t } = useOpsTicket(ticket);
  const slots = useMemo(() => nextSlots(), []);

  const [vendor, setVendor] = useState('');
  const [slot, setSlot] = useState(slots[0]?.starts_at ?? '');
  const { dispatchTo, sending, sent, error: failure } = useDispatch(ticket ?? '');

  const chosen = slots.find((s) => s.starts_at === slot);

  if (loading) {
    return (
      <>
        <BackHeader title="Dispatch a vendor" onBack={goBack} />
        <Screen><View style={s.wait}><ActivityIndicator /></View></Screen>
      </>
    );
  }

  if (!t) {
    return (
      <>
        <BackHeader title="Dispatch a vendor" onBack={goBack} />
        <Screen><Card><Text style={s.note}>{error ?? 'That job is no longer open.'}</Text></Card></Screen>
      </>
    );
  }

  if (sent) {
    return (
      <>
        <BackHeader title="Vendor dispatched" onBack={goBack} />
        <Screen>
          <Toast text="Scheduled — the tenant sees it on their own ticket" />
          <Card>
            <Text style={s.h}>{vendor}</Text>
            <View style={{ marginTop: space(4) }}>
              <KeyValue k="Job" v={t.title} />
              <KeyValue k="Unit" v={t.unit ?? '—'} />
              <KeyValue k="Slot" v={chosen?.label ?? slot} last />
            </View>
            <Text style={s.note}>
              The slot is provisional until the tenant accepts it. Nothing is billable before
              the technician is let in.
            </Text>
          </Card>
          <Button label="Done" onPress={goBack} style={{ marginHorizontal: space(4) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Dispatch a vendor" subtitle={t.title} onBack={goBack} />
      <Screen>
        <Card>
          <Text style={s.h}>Who is going?</Text>
          <Text style={s.sub}>
            The name is recorded on the ticket and shown to the tenant, so send somebody you
            would be willing to name.
          </Text>
          <Field label="Vendor" value={vendor} onChange={setVendor}
            placeholder="Kochi Cooling Services" autoCapitalize="words" />
        </Card>

        <Card>
          <Text style={s.h}>Slot</Text>
          {slots.map((sl, i) => (
            <ChoiceRow key={sl.starts_at} label={sl.label} selected={slot === sl.starts_at}
              onPress={() => setSlot(sl.starts_at)} last={i === slots.length - 1} />
          ))}
          <Text style={s.note}>
            Times are Indian Standard Time, and nothing already gone is offered.
          </Text>
        </Card>

        {failure ? <Text style={s.error} accessibilityRole="alert">{failure}</Text> : null}
      </Screen>

      <ActionBar>
        <Button label="Cancel" tone="secondary" onPress={goBack} style={{ flex: 1 }} />
        <Button
          label={sending ? 'Sending…' : 'Send job order'}
          onPress={() => dispatchTo(vendor, slot)}
          disabled={sending || !vendor.trim() || !slot}
          style={{ flex: 2 }}
        />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  wait: { paddingVertical: space(6), alignItems: 'center' },
  h: { ...font.h3, color: color.inkStrong },
  sub: { ...font.small, color: color.inkSoft, marginTop: 5, lineHeight: 18 },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
  error: { ...font.small, color: color.negative, marginHorizontal: space(4), marginTop: space(2) },
});
