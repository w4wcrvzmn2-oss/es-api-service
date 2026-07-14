using System;
using System.Collections.Generic;
using System.Data;
using System.Data.SQLite;
using System.Globalization;
using System.Linq;
using System.Net.Http.Headers;
using System.Net.Http;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Threading.Tasks;
using System.Windows.Forms;
using Электронная_Фармация.Classes;
using Электронная_Фармация.Properties;


namespace Электронная_Фармация.Classes
{
    public class DataSender
    {
        private const string SqliteConnectionString = "Data Source=efClient.db;Version=3;New=False;";
        private const string ApiBaseUrlEnvironmentVariable = "ELF_API_BASE_URL";
        private const string DefaultBaseUrl = "http://195.34.241.84:9988";
        private const string BuyerOrdersEndpoint = "/api/buyer/orders";

        private string _baseUrl;
        private string _token;

        public async Task Main()
        {
            _token = Settings.Default.stringToken;
            _baseUrl = GetBaseUrl();

            if (string.IsNullOrWhiteSpace(_token))
            {
                MessageBox.Show("Не найден токен авторизации.");
                return;
            }

            var orders = FetchDataFromSql(SqliteConnectionString);
            if (orders.Count == 0)
            {
                MessageBox.Show("Нет новых заказов для отправки.");
                return;
            }

            foreach (var order in orders)
            {
                string json = JsonSerializer.Serialize(order);
                await SendPostRequest(json);
            }
        }

        public List<BuyerOrderRequest> FetchDataFromSql(string connectionString)
        {
            var orders = new Dictionary<string, BuyerOrderRequest>();

            using (SQLiteConnection connection = new SQLiteConnection(connectionString))
            {
                connection.Open();
                string query = @"
SELECT
    Orders.Id_Order,
    OrderItems.guid_es,
    OrderItems.Zakaz,
    Orders.Comment
FROM Orders
INNER JOIN OrderItems ON OrderItems.Id_Order = Orders.Id_Order
WHERE Orders.OrderState = 'НОВЫЙ'
ORDER BY Orders.Id_Order
";
                using (SQLiteCommand command = new SQLiteCommand(query, connection))
                {
                    using (SQLiteDataReader reader = command.ExecuteReader())
                    {
                        while (reader.Read())
                        {
                            string orderId = reader["Id_Order"].ToString().Trim();
                            string supplierPriceId = reader["guid_es"].ToString().Trim();
                            decimal qty = ReadDecimal(reader["Zakaz"]);

                            if (string.IsNullOrWhiteSpace(supplierPriceId))
                            {
                                throw new InvalidOperationException($"В заказе {orderId} есть позиция без supplier_price_id.");
                            }
                            if (qty <= 0)
                            {
                                throw new InvalidOperationException($"В заказе {orderId} количество должно быть больше нуля.");
                            }

                            if (!orders.TryGetValue(orderId, out BuyerOrderRequest order))
                            {
                                order = new BuyerOrderRequest
                                {
                                    Comment = reader["Comment"] == DBNull.Value ? null : reader["Comment"].ToString()
                                };
                                orders.Add(orderId, order);
                            }

                            order.Items.Add(new BuyerOrderItem
                            {
                                SupplierPriceId = supplierPriceId,
                                Qty = qty
                            });
                        }
                    }
                }
            }

            return orders.Values.ToList();
        }

        public async Task SendPostRequest(string json)
        {
            using (HttpClient client = new HttpClient())
            {
                client.BaseAddress = new Uri(_baseUrl);
                client.DefaultRequestHeaders.Authorization = new AuthenticationHeaderValue("Bearer", _token);

                var content = new StringContent(json, Encoding.UTF8, "application/json");
                HttpResponseMessage response = await client.PostAsync(BuyerOrdersEndpoint, content);
                string responseString = await response.Content.ReadAsStringAsync();

                if (!response.IsSuccessStatusCode)
                {
                    MessageBox.Show($"Ошибка отправки заказа: {(int)response.StatusCode} {response.ReasonPhrase}\n{responseString}");
                    return;
                }

                MessageBox.Show(responseString);
            }
        }

        private static string GetBaseUrl()
        {
            string baseUrl = Environment.GetEnvironmentVariable(ApiBaseUrlEnvironmentVariable);
            return string.IsNullOrWhiteSpace(baseUrl) ? DefaultBaseUrl : baseUrl.TrimEnd('/');
        }

        private static decimal ReadDecimal(object value)
        {
            if (value == null || value == DBNull.Value)
            {
                return 0;
            }

            string text = value.ToString();
            if (decimal.TryParse(text, NumberStyles.Number, CultureInfo.InvariantCulture, out decimal invariantValue))
            {
                return invariantValue;
            }
            if (decimal.TryParse(text, NumberStyles.Number, CultureInfo.CurrentCulture, out decimal currentCultureValue))
            {
                return currentCultureValue;
            }

            throw new InvalidOperationException($"Некорректное количество: {text}");
        }
    }

    public class BuyerOrderRequest
    {
        [JsonPropertyName("location_id")]
        public string LocationId { get; set; }

        [JsonPropertyName("comment")]
        public string Comment { get; set; }

        [JsonPropertyName("items")]
        public List<BuyerOrderItem> Items { get; set; } = new List<BuyerOrderItem>();
    }

    public class BuyerOrderItem
    {
        [JsonPropertyName("supplier_price_id")]
        public string SupplierPriceId { get; set; }

        [JsonPropertyName("qty")]
        public decimal Qty { get; set; }
    }
}
