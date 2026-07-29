import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Field, ChoiceRow, Button, ActionBar, KeyValue,
  StatusPill, Toast, SwitchRow, Timeline,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { listings } from '../src/data/mock';

/**
 * An application.
 *
 * Indian rental applications usually happen over WhatsApp and a phone call,
 * which is why nobody can prove what was agreed. This asks once, in a form
 * both sides can see later.
 */

export default function Apply() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const l = listings.find((x) => x.id === id) ?? listings[0];

  const [occupants, setOccupants] = useState('2');
  const [employment, setEmployment] = useState('Salaried');
  const [moveIn, setMoveIn] = useState('');
  const [note, setNote] = useState('');
  const [consent, setConsent] = useState(false);
  const [sent, setSent] = useState(false);

  if (sent) {
    return (
      <>
        <BackHeader title="Application sent" onBack={() => router.replace('/(tabs)/enquiries')} />
        <Screen>
          <Toast text="With the lister — a decision usually takes two days" />
          <Card>
            <StatusPill text="Applied" tone="amber" dot />
            <Text style={s.h1}>{l.title}</Text>
            <Text style={s.sub}>{l.locality} · {inr(l.rentPaise, { noPaise: true })} per month</Text>
            <View style={{ marginTop: space(4) }}>
              <KeyValue k="Occupants" v={occupants} />
              <KeyValue k="Employment" v={employment} />
              <KeyValue k="Move-in" v={moveIn || 'As soon as possible'} />
              <KeyValue k="Deposit needed" v={inr(l.depositPaise, { noPaise: true })} last />
            </View>
            <Text style={s.note}>
              Nothing is payable now. If you are accepted, the deposit and first month are paid
              through Dwellm8, receipted, and held against the tenancy — never in cash to a stranger.
            </Text>
          </Card>
          <Card>
            <Text style={s.h}>What the lister sees</Text>
            <Timeline
              items={[
                { at: 'Now', what: 'Your application, and that you attended the inspection' },
                { at: 'Now', what: 'Employment type and occupants — not your salary figure' },
                { at: 'On request', what: 'Documents you choose to share, one at a time' },
                { at: 'Never', what: 'Your Aadhaar number, which we do not store', done: false },
              ]}
            />
          </Card>
          <Button label="Done" onPress={() => router.replace('/(tabs)/enquiries')} style={{ marginHorizontal: space(4) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Apply" subtitle={`${l.title} · ${inr(l.rentPaise, { noPaise: true })}`} onBack={() => router.back()} />
      <Screen>
        <Card>
          <Field label="How many people will live here?" value={occupants} onChange={setOccupants} keyboardType="numeric" />
          <Field label="When would you move in?" value={moveIn} onChange={setMoveIn} placeholder="05 Aug 2026" />
        </Card>

        <Card>
          <Text style={s.h}>Employment</Text>
          {['Salaried', 'Self-employed', 'Student', 'Retired'].map((e, i) => (
            <ChoiceRow key={e} label={e} selected={employment === e} onPress={() => setEmployment(e)} last={i === 3} />
          ))}
        </Card>

        <Card>
          <Field
            label="Anything the lister should know?"
            value={note}
            onChange={setNote}
            placeholder="We have a small dog, house-trained. Happy to pay a higher deposit."
            multiline
          />
          <SwitchRow
            label="Share my verified profile"
            hint="Your name, employment type and inspection attendance. Never your salary or ID number."
            value={consent}
            onChange={setConsent}
            last
          />
        </Card>
      </Screen>

      <ActionBar>
        <Button label="Cancel" tone="secondary" onPress={() => router.back()} style={{ flex: 1 }} />
        <Button label="Send application" onPress={() => setSent(true)} disabled={!consent || !occupants} style={{ flex: 1.6 }} />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  h1: { ...font.h2, color: color.inkStrong, marginTop: space(3) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  sub: { ...font.body, color: color.inkSoft, marginTop: 4 },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});
