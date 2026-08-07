import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, StatusPill, Button, ActionBar, PhotoStrip,
  Field, Toast, ChoiceRow, ListRow, Timeline,
  color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import { approvals, inr, properties } from '../src/data/mock';

/**
 * A spend approval, decided from the sofa.
 *
 * The owner is being asked for money, so the screen leads with what it is for,
 * what it costs, and what happens if they say no — not with a form. Declining
 * requires a reason, because the manager has to tell somebody something.
 */

export default function Approve() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { id } = useLocalSearchParams<{ id?: string }>();
  const a = approvals.find((x) => x.id === id) ?? approvals[0];
  const p = properties.find((x) => x.id === a.propertyId)!;

  const [reason, setReason] = useState('');
  const [decided, setDecided] = useState<'Approved' | 'Declined' | null>(null);

  const options =
    a.kind === 'renewal'
      ? [
          { vendor: 'Hold at ₹42,000', paise: 4_20_00_00, note: 'No rise. Safest way to keep a reliable tenant.' },
          { vendor: 'Raise to ₹44,100', paise: 4_41_00_00, note: 'Your manager’s recommendation — 5%, in line with the agreement' },
          { vendor: 'Ask ₹45,500', paise: 4_55_00_00, note: 'Market rate. Risks a move-out and about ₹38,000 to re-let.' },
        ]
      : a.kind === 'offer'
      ? [
          { vendor: 'Accept ₹29,000', paise: 2_90_00_00, note: 'Above your asking rent, moves in 5 August' },
          { vendor: 'Counter at ₹30,000', paise: 3_00_00_00, note: 'Risks losing them — the other applicant offered ₹28,000' },
        ]
      : [
          { vendor: a.vendor, paise: a.quotePaise, note: 'Your manager’s recommendation · on panel' },
          ...(a.alternatives ?? []),
        ];
  // The manager's recommendation is pre-selected: it is the option most owners
  // want, and the others are there for the ones who do not.
  const recommended = a.kind === 'renewal' ? options[1].vendor : options[0].vendor;
  const [choice, setChoice] = useState<string>(recommended);
  const picked = options.find((o) => o.vendor === choice) ?? options[0];

  if (decided) {
    return (
      <>
        <BackHeader title={decided} onBack={goBack} />
        <Screen>
          <Toast text={decided === 'Approved' ? 'Your manager has been told — work can start' : 'Sent back to your manager'} />
          <Card>
            <StatusPill text={decided} tone={decided === 'Approved' ? 'green' : 'red'} dot />
            <Text style={s.h1}>{a.title}</Text>
            <Text style={s.sub}>{p.address}</Text>
            <View style={{ marginTop: space(4) }}>
              <KeyValue k={a.kind === 'spend' ? 'Vendor' : a.kind === 'renewal' ? 'Tenant' : 'Applicant'} v={a.vendor} />
              <KeyValue k={a.kind === 'spend' ? 'Amount' : 'Rent'} v={inr(picked.paise)} />
              <KeyValue k="Your choice" v={picked.vendor} />
              <KeyValue
                k="What happens now"
                v={a.kind === 'spend' ? 'Work is instructed and appears on your August statement'
                  : a.kind === 'renewal' ? 'Terms are put to the tenant; nothing binds until they eSign'
                  : 'Agreement drafted, e-stamped and eSigned before move-in'}
                last
              />
            </View>
            {decided === 'Approved' ? (
              <Text style={s.note}>
                Nothing is paid until the work is done and signed off. If the job comes in under the
                quote, you are charged the lower figure.
              </Text>
            ) : (
              <Text style={s.note}>Reason recorded: {reason || 'none given'}</Text>
            )}
          </Card>
          <Button label="Done" onPress={goBack} style={{ marginHorizontal: space(4) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title={a.kind === "spend" ? "Approve spend" : a.kind === "renewal" ? "Renewal" : "Letting offer"} subtitle={p.address} onBack={goBack} />
      <Screen>
        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <StatusPill text={a.urgency} tone={a.urgency === 'Emergency' ? 'red' : a.urgency === 'Urgent' ? 'amber' : 'neutral'} />
            <View style={{ flex: 1 }} />
            <Text style={s.amount}>{inr(a.quotePaise)}</Text>
          </View>
          <Text style={s.h1}>{a.title}</Text>
          <Text style={s.body}>{a.managerNote}</Text>
          <PhotoStrip count={a.photos} />
          <View style={{ marginTop: space(4) }}>
            <KeyValue k="Raised" v={a.raised} />
            <KeyValue k="Your manager" v={p.manager} />
            <KeyValue k="Who bears it" v={a.liability} tone={a.liability === 'Owner' ? 'amber' : 'green'} last />
          </View>
        </Card>

        <Card>
          <Text style={s.h}>{a.kind === 'spend' ? 'Your options' : 'What would you like to do?'}</Text>
          {options.map((o, i) => (
            <ChoiceRow
              key={o.vendor}
              label={a.kind === 'spend' ? `${o.vendor} — ${inr(o.paise, { noPaise: true })}` : o.vendor}
              hint={o.note}
              selected={choice === o.vendor}
              onPress={() => setChoice(o.vendor)}
              last={i === options.length - 1}
            />
          ))}
        </Card>

        <Card>
          <Text style={s.h}>If you say no</Text>
          <Text style={s.body}>
            {a.kind === 'spend'
              ? 'The tenant keeps a geyser that trips the RCB. Under the agreement this is an owner-borne asset failure, so the request comes back — usually with a larger number once the element fails completely.'
              : a.kind === 'renewal'
              ? 'Notice is served and the flat is re-let. Budget about ₹38,000 for the empty weeks and the new letting fee, and expect four to six weeks of vacancy in this building.'
              : 'The flat stays empty at ₹916 a day while your manager works the other applicant, who offered ₹1,000 less.'}
          </Text>
          <Field
            label="Reason (only needed if you decline)"
            value={reason}
            onChange={setReason}
            placeholder="Get a second quote from the AMC vendor first"
            multiline
          />
        </Card>

        <Card>
          <Text style={s.h}>What happens next</Text>
          <Timeline
            items={
              a.kind === 'spend'
                ? [
                    { at: 'On approval', what: 'Manager instructs the vendor and books a slot with the tenant' },
                    { at: 'On site', what: 'Work starts only when the tenant enters their code' },
                    { at: 'On completion', what: 'Photos and the invoice land against this job' },
                    { at: 'Next payout', what: 'Deducted from your statement, itemised', done: false },
                  ]
                : a.kind === 'renewal'
                ? [
                    { at: 'Same day', what: 'Your terms are put to the tenant' },
                    { at: 'Within a week', what: 'They accept, negotiate, or give notice' },
                    { at: 'On acceptance', what: 'Fresh agreement e-stamped and eSigned' },
                    { at: '15 Apr 2027', what: 'New term begins at the agreed rent', done: false },
                  ]
                : [
                    { at: 'Same day', what: 'Agreement drafted with the terms you accepted' },
                    { at: 'Within 2 days', what: 'e-stamp paid, both parties eSign with Aadhaar' },
                    { at: 'Move-in day', what: 'Joint inspection, photographs, keys handed over' },
                    { at: 'Day one', what: 'Deposit acknowledged and the first invoice raised', done: false },
                  ]
            }
          />
        </Card>
      </Screen>

      <ActionBar>
        <Button label="Decline" tone="secondary" onPress={() => setDecided('Declined')} disabled={!reason} style={{ flex: 1 }} />
        <Button
          label={a.kind === 'spend' ? `Approve ${inr(picked.paise, { noPaise: true })}` : 'Confirm'}
          onPress={() => setDecided('Approved')}
          style={{ flex: 1.6 }}
        />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  h1: { ...font.h2, color: color.inkStrong, marginTop: space(3) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  sub: { ...font.body, color: color.inkSoft, marginTop: 4 },
  amount: { ...font.h2, color: color.inkStrong },
  body: { ...font.body, color: color.ink, lineHeight: 22, marginTop: space(3) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});
