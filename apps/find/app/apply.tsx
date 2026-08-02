import React, { useMemo, useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Field, ChoiceRow, Button, ActionBar, KeyValue,
  StatusPill, Toast, SwitchRow, Timeline, ApiError,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { api, prospectToken, useListing } from '../src/data/source';

/**
 * An application (#142).
 *
 * Indian rental applications usually happen over WhatsApp and a phone call,
 * which is why nobody can prove what was agreed. This asks once, in a form
 * both sides can see later. Live mode files it against the real API — which
 * requires a verified phone, asked for here at the moment it matters and not
 * a screen earlier.
 */

/** "05 Aug 2026", "2026-08-05" or empty → YYYY-MM-DD the API accepts. */
function toApiDate(input: string): string {
  const d = input.trim() ? new Date(input) : new Date(Date.now() + 14 * 86_400_000);
  if (Number.isNaN(d.getTime())) return '';
  return d.toISOString().slice(0, 10);
}

export default function Apply() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const client = useMemo(() => api(), []);
  const { listing: l } = useListing(id);

  const [occupants, setOccupants] = useState('2');
  const [employment, setEmployment] = useState('Salaried');
  const [moveIn, setMoveIn] = useState('');
  const [note, setNote] = useState('');
  const [consent, setConsent] = useState(false);

  const [needsPhone, setNeedsPhone] = useState(false);
  const [phone, setPhone] = useState('');
  const [code, setCode] = useState('');
  const [codeSent, setCodeSent] = useState(false);

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [sent, setSent] = useState(false);

  const submit = async () => {
    if (!client) { setSent(true); return; } // demo: the form is the product
    const when = toApiDate(moveIn);
    if (!when) { setError('The move-in date did not parse — try 2026-08-20.'); return; }
    setBusy(true); setError('');
    try {
      const token = await prospectToken(client);
      await client.applyToListing(token, String(id), {
        moveIn: when, termMonths: 11,
        note: [`Occupants: ${occupants}`, `Employment: ${employment}`, note.trim()]
          .filter(Boolean).join('\n'),
      });
      setSent(true);
    } catch (e) {
      if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
        setNeedsPhone(true);
      } else {
        setError(e instanceof Error ? e.message : 'The application did not go through.');
      }
    } finally {
      setBusy(false);
    }
  };

  const sendCode = async () => {
    if (!client) return;
    setBusy(true); setError('');
    try {
      const token = await prospectToken(client);
      await client.prospectVerify(token, phone.trim());
      setCodeSent(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not send the code.');
    } finally {
      setBusy(false);
    }
  };

  const confirmCode = async () => {
    if (!client) return;
    setBusy(true); setError('');
    try {
      const token = await prospectToken(client);
      await client.prospectConfirm(token, phone.trim(), code.trim());
      setNeedsPhone(false);
      await submit();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'That code did not match.');
    } finally {
      setBusy(false);
    }
  };

  if (!l) return null;

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
              <KeyValue k="Move-in" v={moveIn || 'In about two weeks'} />
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

  if (needsPhone) {
    return (
      <>
        <BackHeader title="Verify your phone" subtitle="Applying is making contact" onBack={() => setNeedsPhone(false)} />
        <Screen>
          <Card>
            <Text style={s.note}>
              Browsing is anonymous; an application is not. One code to the number the lister
              would call anyway, and your application carries it verified.
            </Text>
            <Field label="Mobile number" value={phone} onChange={setPhone} keyboardType="phone-pad" placeholder="98450 00000" />
            {codeSent ? (
              <Field label="The 6-digit code" value={code} onChange={setCode} keyboardType="numeric" placeholder="000000" />
            ) : null}
            {error ? <Text style={s.err}>{error}</Text> : null}
          </Card>
        </Screen>
        <ActionBar>
          {codeSent ? (
            <Button label={busy ? 'Checking…' : 'Confirm and apply'} onPress={confirmCode} disabled={busy || code.trim().length < 4} style={{ flex: 1 }} />
          ) : (
            <Button label={busy ? 'Sending…' : 'Send the code'} onPress={sendCode} disabled={busy || phone.trim().length < 10} style={{ flex: 1 }} />
          )}
        </ActionBar>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Apply" subtitle={`${l.title} · ${inr(l.rentPaise, { noPaise: true })}`} onBack={() => router.back()} />
      <Screen>
        <Card>
          <Field label="How many people will live here?" value={occupants} onChange={setOccupants} keyboardType="numeric" />
          <Field label="When would you move in?" value={moveIn} onChange={setMoveIn} placeholder="2026-08-20" />
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
          {error ? <Text style={s.err}>{error}</Text> : null}
        </Card>
      </Screen>

      <ActionBar>
        <Button label="Cancel" tone="secondary" onPress={() => router.back()} style={{ flex: 1 }} />
        <Button
          label={busy ? 'Sending…' : 'Send application'}
          onPress={submit}
          disabled={busy || !consent || !occupants}
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
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
  err: { ...font.small, color: '#B4232A', marginTop: space(2) },
});
