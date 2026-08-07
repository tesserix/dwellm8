import { printAgreement, agreementQuestions } from './agreement';

// The owner–manager agreement (#340). The firm fills what only it knows, the
// server fills the property's own address, and what comes back is filed
// against the property so the issued copy sits beside the executed scan.

const api = {
  opsManagementAgreementFields: jest.fn(),
  opsPrintManagementAgreement: jest.fn(),
};

beforeEach(() => {
  api.opsManagementAgreementFields.mockReset().mockResolvedValue({
    fields: ['owner_name', 'management_fee_pct'],
    supplied: ['property_address'],
    sale_notice_months: 4,
  });
  api.opsPrintManagementAgreement.mockReset().mockResolvedValue({
    filename: 'management-agreement-bpg.pdf',
    content_type: 'application/pdf',
    pdf_base64: 'JVBERi0=',
    document_id: 'd1',
    download_url: 'https://signed.get/agreement',
  });
});

it('asks only for what the firm knows, never the address the register holds', async () => {
  const asked = await agreementQuestions(api as never);

  expect(asked.map((q) => q.field)).toEqual(['owner_name', 'management_fee_pct']);
  expect(asked.map((q) => q.field)).not.toContain('property_address');
  expect(asked[0].label).toBe('Owner name');
});

it('prints the agreement and hands back where to read it', async () => {
  const out = await printAgreement(api as never, 'p1',
    { owner_name: 'Anjali Menon', management_fee_pct: '8' });

  expect(api.opsPrintManagementAgreement).toHaveBeenCalledWith('p1',
    { owner_name: 'Anjali Menon', management_fee_pct: '8' });
  expect(out.downloadUrl).toBe('https://signed.get/agreement');
  expect(out.filename).toBe('management-agreement-bpg.pdf');
});

// Printing with a field blank is what puts "{{management_fee_pct}}" into a
// signed instrument, so the app refuses before the request is made.
it('will not print with a field left blank', async () => {
  await expect(printAgreement(api as never, 'p1',
    { owner_name: 'Anjali Menon', management_fee_pct: '  ' }))
    .rejects.toThrow(/management fee/i);
  expect(api.opsPrintManagementAgreement).not.toHaveBeenCalled();
});
