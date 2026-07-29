import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Button, ActionBar, PhotoStrip, Field, ChoiceRow,
  KeyValue, Toast, StatusPill, SwitchRow, SyncBadge,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { jobs, parts } from '../src/data/mock';

/**
 * Completion — evidence, parts and sign-off.
 *
 * The sign-off is the technician's protection as much as the tenant's: no
 * settlement happens without it, and no invoice can appear for work nobody
 * confirmed.
 */

export default function Complete() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const j = jobs.find((x) => x.id === id) ?? jobs[0];

  const [used, setUsed] = useState<string[]>(['p1', 'p2']);
  const [outcome, setOutcome] = useState('Fixed');
  const [notes, setNotes] = useState('');
  const [signoff, setSignoff] = useState(true);
  const [done, setDone] = useState(false);

  const partsPaise = parts.filter((p) => used.includes(p.id)).reduce((a, p) => a + p.pricePaise, 0);
  const total = j.underAmc ? 0 : j.payPaise + partsPaise;

  const toggle = (pid: string) =>
    setUsed((u) => (u.includes(pid) ? u.filter((x) => x !== pid) : [...u, pid]));

  if (done) {
    return (
      <>
        <BackHeader title="Job completed" onBack={() => router.replace('/(tabs)')} />
        <Screen>
          <Toast text="Submitted — the manager and tenant have been told" />
          <Card>
            <StatusPill text="Awaiting settlement" tone="amber" />
            <Text style={s.title}>{j.title}</Text>
            <Text style={s.sub}>{j.unit}</Text>
            <View style={{ marginTop: space(4) }}>
              <KeyValue k="Time on site" v="1 h 12 m" />
              <KeyValue k="Outcome" v={outcome} />
              <KeyValue k="Parts" v={inr(partsPaise)} />
              <KeyValue k="Labour" v={inr(j.payPaise)} />
              <KeyValue k="Total" v={j.underAmc ? 'Under AMC — no charge' : inr(total)} tone="green" last />
            </View>
            <Text style={s.note}>
              Settles on 05 Aug with the rest of the week, less 1% TDS. If the tenant disputes the
              work, your photos and the OTP timestamp are what settle it.
            </Text>
          </Card>
          <Button label="Back to today" onPress={() => router.replace('/(tabs)')} style={{ marginHorizontal: space(4) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Complete the job" subtitle={j.unit} onBack={() => router.back()} />
      <Screen>
        <SyncBadge queued={2} />

        <Card>
          <Text style={s.h}>Evidence</Text>
          <Text style={s.body}>Photograph the finished work from the same angle the tenant used.</Text>
          <PhotoStrip count={2} onAdd={() => {}} />
        </Card>

        <Card>
          <Text style={s.h}>Outcome</Text>
          {['Fixed', 'Temporary fix — needs a return visit', 'Not a fault — advised the tenant', 'Could not access'].map((o, i) => (
            <ChoiceRow key={o} label={o} selected={outcome === o} onPress={() => setOutcome(o)} last={i === 3} />
          ))}
        </Card>

        <Card>
          <Text style={s.h}>Parts used</Text>
          {parts.map((p, i) => (
            <ChoiceRow
              key={p.id}
              label={p.name}
              hint={inr(p.pricePaise)}
              selected={used.includes(p.id)}
              onPress={() => toggle(p.id)}
              last={i === parts.length - 1}
            />
          ))}
          <View style={{ marginTop: space(3) }}>
            <KeyValue k="Parts" v={inr(partsPaise)} />
            <KeyValue k="Labour" v={inr(j.payPaise)} />
            <KeyValue k="Total" v={j.underAmc ? 'Under AMC' : inr(total)} tone="green" last />
          </View>
        </Card>

        <Card>
          <Field label="Notes for the manager" value={notes} onChange={setNotes} placeholder="Inlet valve was seeping and has been replaced…" multiline />
          <SwitchRow
            label="Tenant signed off on site"
            hint="Required before the job can be settled"
            value={signoff}
            onChange={setSignoff}
            last
          />
        </Card>
      </Screen>

      <ActionBar>
        <Button label="Save draft" tone="secondary" onPress={() => router.back()} style={{ flex: 1 }} />
        <Button label="Submit" onPress={() => setDone(true)} disabled={!signoff} style={{ flex: 1.6 }} />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong },
  body: { ...font.small, color: color.inkSoft, marginTop: 6, lineHeight: 18 },
  title: { ...font.h3, color: color.inkStrong, marginTop: space(3) },
  sub: { ...font.body, color: color.inkSoft, marginTop: 3 },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});
