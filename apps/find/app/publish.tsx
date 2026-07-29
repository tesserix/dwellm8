import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Field, ChoiceRow, Button, ActionBar, KeyValue,
  PhotoStrip, StatusPill, Toast, SwitchRow, ProgressBar, Timeline,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { BOOST_PAISE, LISTING_DAYS } from '../src/data/mock';

/**
 * Publishing a listing.
 *
 * Four steps, each of which asks for one kind of thing: what the home is, what
 * it costs, what it looks like, and proof that it is yours to let. The proof
 * step cannot be skipped, which is the whole point.
 */

const STEPS = ['The home', 'The money', 'Photographs', 'Proof'];

export default function Publish() {
  const router = useRouter();
  const [step, setStep] = useState(0);
  const [done, setDone] = useState(false);
  const [toast, setToast] = useState<string | null>(null);

  const [address, setAddress] = useState('');
  const [locality, setLocality] = useState('');
  const [kind, setKind] = useState('Flat');
  const [bhk, setBhk] = useState('2 BHK');
  const [furnishing, setFurnishing] = useState('Semi-furnished');
  const [rent, setRent] = useState('');
  const [deposit, setDeposit] = useState('');
  const [available, setAvailable] = useState('');
  const [boost, setBoost] = useState(false);

  const rentPaise = Math.round((Number(rent.replace(/[^0-9]/g, '')) || 0) * 100);
  const depositPaise = Math.round((Number(deposit.replace(/[^0-9]/g, '')) || 0) * 100);

  if (done) {
    return (
      <>
        <BackHeader title="Sent for verification" onBack={() => router.replace('/(tabs)/list')} />
        <Screen>
          <Toast text="We will check the documents and publish within a day" />
          <Card>
            <StatusPill text="In verification" tone="amber" dot />
            <Text style={s.h1}>{address || 'Your listing'}</Text>
            <Text style={s.sub}>{locality}</Text>
            <View style={{ marginTop: space(4) }}>
              <KeyValue k="Rent" v={rentPaise ? inr(rentPaise, { noPaise: true }) : '—'} />
              <KeyValue k="Deposit" v={depositPaise ? inr(depositPaise, { noPaise: true }) : '—'} />
              <KeyValue k="Runs for" v={`${LISTING_DAYS} days once live`} />
              <KeyValue k="Promotion" v={boost ? `${inr(BOOST_PAISE, { noPaise: true })} · 14 days at the top` : 'Not taken'} last />
            </View>
          </Card>
          <Card>
            <Text style={s.h}>What happens now</Text>
            <Timeline
              items={[
                { at: 'Within an hour', what: 'Address geocoded and matched to your document' },
                { at: 'Within a day', what: 'Ownership and identity checked by a person' },
                { at: 'On approval', what: 'Live in search, and in the alerts of anyone waiting' },
                { at: `Day ${LISTING_DAYS}`, what: 'Comes down unless you re-publish it', done: false },
              ]}
            />
          </Card>
          <Button label="Back to my listings" onPress={() => router.replace('/(tabs)/list')} style={{ marginHorizontal: space(4) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title="List a property" subtitle={`Step ${step + 1} of 4 · ${STEPS[step]}`} onBack={() => (step ? setStep(step - 1) : router.back())} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}
        <View style={{ marginHorizontal: space(4), marginTop: space(3) }}>
          <ProgressBar pct={((step + 1) / 4) * 100} tint={color.accent} />
        </View>

        {step === 0 ? (
          <>
            <Card>
              <Field label="Address" value={address} onChange={setAddress} placeholder="B-12, Kumar Prithvi" />
              <Field label="Locality and city" value={locality} onChange={setLocality} placeholder="Baner, Pune" />
            </Card>
            <Card>
              <Text style={s.h}>What kind of home?</Text>
              {['Flat', 'Independent house', 'PG bed', 'Commercial'].map((k, i) => (
                <ChoiceRow key={k} label={k} selected={kind === k} onPress={() => setKind(k)} last={i === 3} />
              ))}
            </Card>
            <Card>
              <Text style={s.h}>Size and furnishing</Text>
              {['1 BHK', '2 BHK', '3 BHK', '4 BHK or more'].map((b, i) => (
                <ChoiceRow key={b} label={b} selected={bhk === b} onPress={() => setBhk(b)} last={i === 3} />
              ))}
              {['Unfurnished', 'Semi-furnished', 'Fully furnished'].map((f, i) => (
                <ChoiceRow key={f} label={f} selected={furnishing === f} onPress={() => setFurnishing(f)} last={i === 2} />
              ))}
            </Card>
          </>
        ) : null}

        {step === 1 ? (
          <>
            <Card>
              <Field label="Monthly rent (₹)" value={rent} onChange={setRent} placeholder="29000" keyboardType="numeric" />
              <Field label="Security deposit (₹)" value={deposit} onChange={setDeposit} placeholder="58000" keyboardType="numeric" />
              <Field label="Available from" value={available} onChange={setAvailable} placeholder="05 Aug 2026" />
              {rentPaise > 0 ? (
                <Text style={s.hint}>
                  Comparable 2 BHKs in Baner let at ₹27,000 – ₹31,000. Yours is priced{' '}
                  {rentPaise > 3_00_00_00 ? 'above' : 'inside'} that range.
                </Text>
              ) : null}
            </Card>
            <Card>
              <Text style={s.h}>Would you like us to manage it?</Text>
              <Text style={s.body}>
                Rent collection, repairs, inspections and the owner statement, for 2.99% at payout.
                Listings we manage carry a badge, because the tenancy behind them is on our ledger.
              </Text>
              <Button label="Tell me more" tone="secondary" onPress={() => setToast('A manager will call you after the listing goes live')} style={{ marginTop: space(4) }} />
            </Card>
          </>
        ) : null}

        {step === 2 ? (
          <Card>
            <Text style={s.h}>Photographs</Text>
            <Text style={s.body}>
              At least four, taken on site. Listings with a photograph of every room get roughly
              three times the enquiries — and we check that they are yours, not another portal's.
            </Text>
            <PhotoStrip count={3} onAdd={() => setToast('Camera would open here')} />
            <View style={{ marginTop: space(4) }}>
              <KeyValue k="Living room" v="Added" tone="green" />
              <KeyValue k="Kitchen" v="Added" tone="green" />
              <KeyValue k="Bedrooms" v="1 of 2" tone="amber" />
              <KeyValue k="Bathroom" v="Missing" tone="red" last />
            </View>
          </Card>
        ) : null}

        {step === 3 ? (
          <>
            <Card>
              <Text style={s.h}>Prove it is yours to let</Text>
              <Text style={s.body}>
                One ownership document and one ID. A managing agency can upload the management
                agreement instead. Nothing is published until a person has checked both.
              </Text>
              <View style={{ marginTop: space(3) }}>
                <KeyValue k="Khata or sale deed" v="Uploaded" tone="green" />
                <KeyValue k="Government ID" v="Uploaded" tone="green" />
                <KeyValue k="Address match" v="Runs automatically" />
                <KeyValue k="Aadhaar number" v="Never stored" last />
              </View>
            </Card>
            <Card>
              <Text style={s.h}>Promote this listing?</Text>
              <SwitchRow
                label={`Top of search for 14 days — ${inr(BOOST_PAISE, { noPaise: true })}`}
                hint="On comparable Baner listings this has meant about 2.4× the views"
                value={boost}
                onChange={setBoost}
                last
              />
              <Text style={s.note}>
                Promotion moves you up the list and nothing else. It cannot buy a verification
                badge, and a promoted listing is always labelled as one.
              </Text>
            </Card>
          </>
        ) : null}
      </Screen>

      <ActionBar>
        {step > 0 ? <Button label="Back" tone="secondary" onPress={() => setStep(step - 1)} style={{ flex: 1 }} /> : null}
        <Button
          label={step === 3 ? 'Send for verification' : 'Continue'}
          onPress={() => (step === 3 ? setDone(true) : setStep(step + 1))}
          disabled={step === 0 && (!address || !locality)}
          style={{ flex: 1.6 }}
        />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  h1: { ...font.h2, color: color.inkStrong, marginTop: space(3) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3 },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  hint: { ...font.small, color: color.accent, fontWeight: '600' },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});
