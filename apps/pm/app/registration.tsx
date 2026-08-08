import React, { useCallback, useEffect, useState } from 'react';
import { View, Text, StyleSheet, ScrollView, KeyboardAvoidingView, Platform, Pressable } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  AddressLookup, ApiError, Button, Card, ChevronLeft, ChoiceRow, Field, SectionTitle, StatusPill,
  apiFromEnv, color, font, space,
  type AddressSuggestion, type FirmRegistration,
} from '@dwellm8/mobile-shared';
import { useSession } from '../src/auth/session';

/**
 * What a managing firm must hold before it may take somebody else's property
 * on: the constitution's own PAN and a registered office (#282). The server owns
 * the checklist — this screen renders whatever it is told is outstanding rather
 * than restating the rules.
 *
 * Reachable again after onboarding, because a manager who later takes up broking
 * needs somewhere to file the RERA registration they did not need on day one
 * (#359).
 */

const constitutions: { value: string; label: string; hint: string }[] = [
  { value: 'proprietorship', label: 'Sole proprietor', hint: 'You trade in your own name — your own PAN' },
  { value: 'partnership', label: 'Partnership firm', hint: "The firm's PAN and the registered deed" },
  { value: 'llp', label: 'LLP', hint: "The LLP's PAN, incorporation certificate and LLPIN" },
  { value: 'company', label: 'Private/public company', hint: "The company's PAN, CIN and MOA/AOA" },
  { value: 'trust', label: 'Trust', hint: "The trust's PAN and trust deed" },
  { value: 'society', label: 'Society', hint: "The society's PAN and registration certificate" },
];

export default function Registration() {
  const { session, refreshRegistration, signOut } = useSession();
  const router = useRouter();
  const { revisit } = useLocalSearchParams<{ revisit?: string }>();
  const [standing, setStanding] = useState<FirmRegistration | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // The field the server refused, when it named one — the reason belongs under
  // the value it is about, not under the button (#287).
  const [badField, setBadField] = useState('');

  const [legalName, setLegalName] = useState('');
  const [tradeName, setTradeName] = useState('');
  const [constitution, setConstitution] = useState('proprietorship');
  const [pan, setPan] = useState('');
  const [tan, setTan] = useState('');
  const [gstin, setGstin] = useState('');
  const [registrarId, setRegistrarId] = useState('');
  const [line1, setLine1] = useState('');
  const [locality, setLocality] = useState('');
  const [city, setCity] = useState('');
  const [stateCode, setStateCode] = useState('');
  const [pin, setPin] = useState('');
  const [phone, setPhone] = useState('');

  const [reraState, setReraState] = useState('');
  const [reraNumber, setReraNumber] = useState('');
  const [reraFrom, setReraFrom] = useState('');
  const [reraTo, setReraTo] = useState('');

  // No token argument: the session's refreshing source is the only one, so a
  // screen open past the hour does not keep sending the token it opened with.
  const api = useCallback(() => apiFromEnv(), []);

  const findAddress = useCallback(async (q: string) => {
    const client = api();
    return client ? client.searchAddresses(q) : [];
  }, [api]);

  // A picked address overwrites: the point of picking one is not to keep what
  // was half-typed. Only fields the geocoder actually knew are touched.
  const takeAddress = useCallback((a: AddressSuggestion) => {
    setLine1(a.line1 || a.description);
    if (a.locality) setLocality(a.locality);
    if (a.city) setCity(a.city);
    if (a.state_code) setStateCode(a.state_code);
    if (a.pin_code) setPin(a.pin_code);
  }, []);

  const load = useCallback(async () => {
    const client = api();
    if (!client) return;
    try {
      const out = await client.opsRegistration();
      setStanding(out);
      setLegalName((v) => v || out.legal_name || '');
      setTradeName((v) => v || out.trade_name || '');
      if (out.constitution) setConstitution(out.constitution);
      setTan((v) => v || out.tan || '');
      setGstin((v) => v || out.gstin || '');
      setRegistrarId((v) => v || out.registrar_id || '');
      setLine1((v) => v || out.address_line1 || '');
      setLocality((v) => v || out.locality || '');
      setCity((v) => v || out.city || '');
      setStateCode((v) => v || out.state_code || '');
      setPin((v) => v || out.pin_code || '');
      setPhone((v) => v || out.contact_phone || '');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not read the registration');
    }
  }, [api]);

  useEffect(() => { void load(); }, [load]);

  const save = async () => {
    setError('');
    setBadField('');
    setBusy(true);
    try {
      const client = api();
      if (!client) throw new Error('This build has no API configured');
      setStanding(await client.opsSaveRegistration({
        legal_name: legalName.trim(), trade_name: tradeName.trim(), constitution,
        pan: pan.trim(), tan: tan.trim(), gstin: gstin.trim(), registrar_id: registrarId.trim(),
        address_line1: line1.trim(), locality: locality.trim(), city: city.trim(),
        state_code: stateCode.trim(), pin_code: pin.trim(),
        contact_email: session?.email ?? '', contact_phone: phone.trim(),
      }));
      // The PAN never survives the save — the mask comes back instead.
      setPan('');
      await refreshRegistration();
    } catch (e) {
      const said = e instanceof Error ? e.message : 'Those details were refused';
      const field = e instanceof ApiError ? e.field ?? '' : '';
      setBadField(field);
      setError(said);
    } finally {
      setBusy(false);
    }
  };

  const file = async () => {
    setError('');
    setBusy(true);
    try {
      const client = api();
      if (!client) throw new Error('This build has no API configured');
      setStanding(await client.opsFileRegistration({
        state_code: reraState.trim(), number: reraNumber.trim(),
        valid_from: reraFrom.trim(), valid_to: reraTo.trim(),
      }));
      setReraNumber(''); setReraFrom(''); setReraTo('');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'That registration was refused');
    } finally {
      setBusy(false);
    }
  };

  const needsRegistrar = (standing?.fields ?? []).includes('registrar_id')
    || constitution === 'llp' || constitution === 'company';
  const bodyCorporate = constitution !== 'proprietorship' && constitution !== 'individual';
  const refusalFor = (field: string) => (badField === field ? error : undefined);

  return (
    <KeyboardAvoidingView
      style={{ flex: 1, backgroundColor: color.bgTop }}
      behavior={Platform.OS === 'ios' ? 'padding' : 'height'}
    >
      <ScrollView contentContainerStyle={{ paddingTop: space(12), paddingBottom: space(16) }}>
        <View style={s.hero}>
          {revisit ? (
            <Pressable accessibilityRole="button" accessibilityLabel="Back"
              onPress={() => router.back()} hitSlop={10} style={{ marginBottom: space(3) }}>
              <ChevronLeft size={28} w={2.4} />
            </Pressable>
          ) : null}
          <Text style={s.wordmark}>Your firm’s registration</Text>
          <Text style={s.sub}>
            Managing another person’s property for a fee is agency work. These are
            the details and papers that let you hold a mandate.
          </Text>
        </View>

        <Card>
          <SectionTitle>How is the firm constituted?</SectionTitle>
          <Text style={s.note}>
            This decides whose PAN is filed — the entity’s for a firm, your own for
            a sole proprietor — and which documents are asked for.
          </Text>
          {constitutions.map((c, i) => (
            <ChoiceRow
              key={c.value}
              label={c.label}
              hint={c.hint}
              selected={constitution === c.value}
              onPress={() => setConstitution(c.value)}
              last={i === constitutions.length - 1}
            />
          ))}
        </Card>

        <Card>
          <SectionTitle>Statutory details</SectionTitle>
          <Field label="Legal name" value={legalName} onChange={setLegalName}
            placeholder="Menon Estates LLP" autoCapitalize="words" />
          <Field label="Trade name (optional)" value={tradeName} onChange={setTradeName}
            placeholder="Menon Estates" autoCapitalize="words" />
          <Field
            label={bodyCorporate ? "Firm's PAN" : 'Your PAN'}
            value={pan}
            onChange={(v) => setPan(v.toUpperCase().slice(0, 10))}
            placeholder="ABCDE1234F"
            autoCapitalize="characters"
            error={refusalFor('pan')}
          />
          {standing?.pan_masked ? <Text style={s.ok}>On file: {standing.pan_masked}</Text> : null}
          <Field label="TAN (needed to deduct TDS on rent, s.194-I)" value={tan}
            onChange={(v) => setTan(v.toUpperCase().slice(0, 10))} placeholder="BLRM12345C"
            autoCapitalize="characters" error={refusalFor('tan')} />
          <Field label="GSTIN (optional)" value={gstin}
            onChange={(v) => setGstin(v.toUpperCase().slice(0, 15))} placeholder="29ABCDE1234F1Z5"
            autoCapitalize="characters" error={refusalFor('gstin')} />
          {needsRegistrar ? (
            <Field label={constitution === 'llp' ? 'LLPIN' : 'CIN'} value={registrarId}
              onChange={(v) => setRegistrarId(v.toUpperCase())} placeholder="AAA-1234"
              autoCapitalize="characters" />
          ) : null}
        </Card>

        <Card>
          <SectionTitle>Registered office</SectionTitle>
          <Text style={s.note}>
            This is the address notices are served at. Search for it and the
            state code and PIN are filled from the map rather than from memory.
          </Text>
          <AddressLookup search={findAddress} onPick={takeAddress} />
          <Field label="Address" value={line1} onChange={setLine1} placeholder="2nd Floor, Chandra Arcade" />
          <Field label="Locality" value={locality} onChange={setLocality} placeholder="Kadavanthra" />
          <Field label="City" value={city} onChange={setCity} placeholder="Kochi" />
          <Field label="State code" value={stateCode}
            onChange={(v) => setStateCode(v.toUpperCase().slice(0, 2))} placeholder="KL"
            autoCapitalize="characters" />
          <Field label="PIN code" value={pin} onChange={(v) => setPin(v.replace(/[^0-9]/g, '').slice(0, 6))}
            placeholder="682020" keyboardType="numeric" error={refusalFor('pin_code')} />
          <Field label="Contact phone" value={phone} onChange={setPhone}
            placeholder="+919847012345" keyboardType="phone-pad" />
          {error && !badField ? <Text style={s.error} accessibilityRole="alert">{error}</Text> : null}
          <Button label={busy ? 'Saving…' : 'Save details'} onPress={save}
            disabled={busy || legalName.trim().length < 2 || !line1.trim() || !city.trim()
              || stateCode.trim().length !== 2 || pin.trim().length !== 6 || !phone.trim()} />
        </Card>

        <Card>
          <SectionTitle>RERA agent registration</SectionTitle>
          <Text style={s.note}>
            Optional. Section 9 registers an agent who brokers the sale or purchase
            of property in a registered project — letting, collecting rent and
            managing do not need it. File one per state if you take that work on,
            now or later; it expires, so the dates are asked for.
          </Text>
          {(standing?.authorities ?? []).map((a) => (
            <View key={a.id} style={s.filed}>
              <Text style={s.filedText}>{a.state_code} · {a.number}</Text>
              <StatusPill text={`to ${a.valid_to}`} tone="green" />
            </View>
          ))}
          <Field label="State code" value={reraState}
            onChange={(v) => setReraState(v.toUpperCase().slice(0, 2))} placeholder="KL"
            autoCapitalize="characters" />
          <Field label="Registration number" value={reraNumber} onChange={setReraNumber}
            placeholder="K-RERA/AG/123/2026" autoCapitalize="characters" />
          <Field label="Valid from" value={reraFrom} onChange={setReraFrom} placeholder="2026-04-01" />
          <Field label="Valid to" value={reraTo} onChange={setReraTo} placeholder="2031-03-31" />
          <Button label={busy ? 'Filing…' : 'File registration'} onPress={file}
            disabled={busy || reraState.length !== 2 || !reraNumber.trim()
              || reraFrom.length !== 10 || reraTo.length !== 10} />
        </Card>

        <Card>
          <SectionTitle>Documents still needed</SectionTitle>
          {(standing?.outstanding ?? []).length === 0 ? (
            <Text style={s.ok}>Nothing outstanding.</Text>
          ) : (
            (standing?.outstanding ?? []).map((d) => (
              <View key={d.kind} style={s.doc}>
                <Text style={s.docLabel}>{d.label}</Text>
                <Text style={s.docWhy}>{d.why}</Text>
                <Text style={s.docSubject}>
                  {d.subject === 'firm' ? 'For the firm' : 'For you personally'}
                  {d.expires ? ' · expires' : ''}
                </Text>
              </View>
            ))
          )}
        </Card>

        <Pressable onPress={() => void load()} hitSlop={10} style={{ marginTop: space(2) }}>
          <Text style={s.switch}>Refresh</Text>
        </Pressable>

        {/* The longest step of onboarding, with no tab bar behind it: reached
            under the wrong account there was no way out at all (#310). */}
        <Text style={s.filedUnder}>Filing under {session?.email ?? 'this account'}</Text>
        <Pressable onPress={() => void signOut()} hitSlop={10} style={{ marginTop: space(2) }}>
          <Text style={s.switch}>Sign in as somebody else</Text>
        </Pressable>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const s = StyleSheet.create({
  hero: { paddingHorizontal: space(5), marginBottom: space(5) },
  wordmark: { ...font.h1, fontSize: 26, color: color.inkStrong },
  sub: { ...font.small, color: color.inkSoft, marginTop: space(2), lineHeight: 19 },
  note: { ...font.small, color: color.inkSoft, lineHeight: 19, marginBottom: space(3) },
  ok: { ...font.small, color: color.inkSoft, marginBottom: space(3) },
  error: { ...font.small, color: color.negative, marginBottom: space(3) },
  switch: { ...font.label, color: color.accent, textAlign: 'center' },
  filedUnder: { ...font.small, color: color.inkSoft, textAlign: 'center', marginTop: space(4) },
  filed: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginBottom: space(3) },
  filedText: { ...font.body, color: color.inkStrong },
  doc: { marginBottom: space(4) },
  docLabel: { ...font.label, color: color.inkStrong },
  docWhy: { ...font.small, color: color.inkSoft, lineHeight: 18, marginTop: space(1) },
  docSubject: { ...font.small, color: color.inkFaint, marginTop: space(1) },
});
