import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, Button, ActionBar, ChoiceRow, SwitchRow,
  Toast, StatusPill, Timeline,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { mandate, tenancy, totalDue } from '../src/data/mock';

/**
 * UPI Autopay setup.
 *
 * The mandate is the tenant's, not the manager's: the cap, the day and the
 * pause switch are all shown before anything is approved, because a mandate
 * a tenant does not understand is a mandate they cancel.
 */

export default function Autopay() {
  const router = useRouter();
  const [day, setDay] = useState(mandate.debitDay);
  const [remind, setRemind] = useState(true);
  const [done, setDone] = useState(false);

  if (done) {
    return (
      <>
        <BackHeader title="Autopay is on" onBack={() => router.back()} />
        <Screen>
          <Toast text="Mandate approved in your UPI app" />
          <Card>
            <StatusPill text="Active" tone="green" dot />
            <Text style={s.big}>{inr(totalDue, { noPaise: true })} a month</Text>
            <KeyValue k="Debits on" v={`${day} of each month`} />
            <KeyValue k="Cap" v={`${inr(mandate.amountCapPaise, { noPaise: true })} per debit`} />
            <KeyValue k="From" v={mandate.bank} />
            <KeyValue k="Reminder" v={remind ? '2 days before, on WhatsApp' : 'Off'} last />
            <Text style={s.note}>
              You can pause or cancel this from here at any time. If a debit fails, nothing is
              charged to you for the failure and your manager is told the same day.
            </Text>
          </Card>
          <Button label="Done" onPress={() => router.back()} style={{ marginHorizontal: space(4) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Set up autopay" subtitle={tenancy.unit} onBack={() => router.back()} />
      <Screen>
        <Card>
          <Text style={s.h}>Never think about rent again</Text>
          <Text style={s.body}>{mandate.note}</Text>
          <View style={{ marginTop: space(3) }}>
            <KeyValue k="Amount" v={`${inr(totalDue)} per month`} />
            <KeyValue k="Cap you approve" v={inr(mandate.amountCapPaise)} />
            <KeyValue k="Charge to you" v="None — UPI carries no fee" tone="green" last />
          </View>
        </Card>

        <Card>
          <Text style={s.h}>Which day?</Text>
          {[1, 3, 5].map((d, i) => (
            <ChoiceRow
              key={d}
              label={`${d} of the month`}
              hint={d === 5 ? 'Your rent is due on the 5th' : d === 3 ? 'Two days early, safest for salary credits' : 'First working day'}
              selected={day === d}
              onPress={() => setDay(d)}
              last={i === 2}
            />
          ))}
          <SwitchRow
            label="Remind me before each debit"
            hint="WhatsApp, two days ahead"
            value={remind}
            onChange={setRemind}
            last
          />
        </Card>

        <Card>
          <Text style={s.h}>What happens next</Text>
          <Timeline
            items={[
              { at: 'Now', what: 'Your UPI app opens and asks you to approve the mandate' },
              { at: 'Each month', what: `We present the debit on the ${day}` },
              { at: 'Same day', what: 'Receipt lands here and on WhatsApp' },
              { at: 'Any time', what: 'Pause or cancel from this screen', done: false },
            ]}
          />
        </Card>
      </Screen>

      <ActionBar>
        <Button label="Not now" tone="secondary" onPress={() => router.back()} style={{ flex: 1 }} />
        <Button label="Approve in UPI app" onPress={() => setDone(true)} style={{ flex: 1.6 }} />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  big: { ...font.h1, fontSize: 30, color: color.inkStrong, marginVertical: space(3) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});
