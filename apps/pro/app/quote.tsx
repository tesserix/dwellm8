import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Button, ActionBar, ChoiceRow, Field, KeyValue,
  Toast, StatusPill, Timeline, PhotoStrip,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { parts, quotes } from '../src/data/mock';

/** Raise a quote, or read one already raised. */

export default function Quote() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const existing = quotes.find((q) => q.id === id);

  const [used, setUsed] = useState<string[]>([]);
  const [labour, setLabour] = useState('650');
  const [why, setWhy] = useState('');
  const [sent, setSent] = useState(false);

  const partsPaise = parts.filter((p) => used.includes(p.id)).reduce((a, p) => a + p.pricePaise, 0);
  const labourPaise = Math.round((Number(labour.replace(/[^0-9.]/g, '')) || 0) * 100);
  const total = partsPaise + labourPaise;

  if (existing) {
    return (
      <>
        <BackHeader title={existing.id.toUpperCase()} subtitle={existing.job} onBack={() => router.back()} />
        <Screen>
          <Card>
            <StatusPill text={existing.state} tone={existing.state === 'Approved' ? 'green' : 'amber'} />
            <Text style={s.big}>{inr(existing.amountPaise)}</Text>
            <KeyValue k="Raised" v={existing.at} />
            <KeyValue k="Job" v={existing.job} />
            <KeyValue k="Valid until" v="05 Aug 2026" last />
          </Card>
          <Card>
            <Text style={s.h}>Progress</Text>
            <Timeline
              items={[
                { at: existing.at, what: 'You submitted the quote' },
                { at: existing.at, what: 'Manager reviewed it' },
                {
                  at: existing.state === 'Approved' ? existing.at : '—',
                  what: existing.state === 'Approved' ? 'Owner approved — you may start' : 'Waiting on the owner',
                  done: existing.state === 'Approved',
                },
              ]}
            />
          </Card>
        </Screen>
      </>
    );
  }

  if (sent) {
    return (
      <>
        <BackHeader title="Quote sent" onBack={() => router.back()} />
        <Screen>
          <Toast text="With the manager — you will be told when it is decided" />
          <Card>
            <Text style={s.big}>{inr(total)}</Text>
            <KeyValue k="Parts" v={inr(partsPaise)} />
            <KeyValue k="Labour" v={inr(labourPaise)} />
            <KeyValue k="Decision by" v="Manager, or the owner if above ₹10,000" last />
            <Text style={s.note}>Do not start the work until this is approved. Unapproved work cannot be settled.</Text>
          </Card>
          <Button label="Done" onPress={() => router.back()} style={{ marginHorizontal: space(4) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Raise a quote" onBack={() => router.back()} />
      <Screen>
        <Card>
          <Text style={s.h}>What is needed</Text>
          {parts.map((p, i) => (
            <ChoiceRow
              key={p.id}
              label={p.name}
              hint={inr(p.pricePaise)}
              selected={used.includes(p.id)}
              onPress={() => setUsed((u) => (u.includes(p.id) ? u.filter((x) => x !== p.id) : [...u, p.id]))}
              last={i === parts.length - 1}
            />
          ))}
        </Card>

        <Card>
          <Field label="Labour (₹)" value={labour} onChange={setLabour} keyboardType="numeric" />
          <Field label="Why it is needed" value={why} onChange={setWhy} placeholder="Thermostat tested faulty, element scaled…" multiline />
          <Text style={s.h}>Photos of the fault</Text>
          <PhotoStrip count={1} onAdd={() => {}} />
        </Card>

        <Card>
          <KeyValue k="Parts" v={inr(partsPaise)} />
          <KeyValue k="Labour" v={inr(labourPaise)} />
          <KeyValue k="Total" v={inr(total)} tone="green" last />
        </Card>
      </Screen>

      <ActionBar>
        <Button label="Cancel" tone="secondary" onPress={() => router.back()} style={{ flex: 1 }} />
        <Button label="Send quote" onPress={() => setSent(true)} disabled={total <= 0} style={{ flex: 1.6 }} />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  big: { ...font.h1, fontSize: 32, color: color.inkStrong, marginVertical: space(3) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});
